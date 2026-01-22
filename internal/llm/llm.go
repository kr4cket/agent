package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/sashabaranov/go-openai"
	"go.uber.org/zap"
	"testTask/internal/config"
	"testTask/internal/logging"
)

type LLMClient interface {
	Chat(ctx context.Context, messages []Message, tools []Tool) (*ChatResponse, error)
	ChatWithImage(ctx context.Context, messages []Message, imagePath string, tools []Tool) (*ChatResponse, error)
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Tool struct {
	Type        string                 `json:"type"`
	Name        string                 `json:"name,omitempty"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type ChatResponse struct {
	Content      string
	ToolCalls    []ToolCall
	FinishReason string
}

type ToolCall struct {
	ID   string
	Name string
	Args map[string]interface{}
}

type OpenAIClient struct {
	client *openai.Client
	model  string
	config *config.Config
	logger *zap.Logger
}

func New(cfg *config.Config, logger *logging.Logger) LLMClient {
	clientConfig := openai.DefaultConfig(cfg.OpenAI.APIKey)
	if cfg.OpenAI.BaseURL != "" {
		clientConfig.BaseURL = cfg.OpenAI.BaseURL
	}

	client := openai.NewClientWithConfig(clientConfig)

	return &OpenAIClient{
		client: client,
		model:  cfg.OpenAI.Model,
		config: cfg,
		logger: logger.Console(),
	}
}

func (c *OpenAIClient) Chat(ctx context.Context, messages []Message, tools []Tool) (*ChatResponse, error) {
	return c.chatWithRetry(ctx, messages, tools, "")
}

func (c *OpenAIClient) ChatWithImage(ctx context.Context, messages []Message, imagePath string, tools []Tool) (*ChatResponse, error) {
	return c.chatWithRetry(ctx, messages, tools, imagePath)
}

func (c *OpenAIClient) chatWithRetry(ctx context.Context, messages []Message, tools []Tool, imagePath string) (*ChatResponse, error) {
	var result *ChatResponse
	var lastErr error

	timeoutCtx, cancel := context.WithTimeout(ctx, c.config.OpenAI.Timeout)
	defer cancel()

	bo := backoff.NewExponentialBackOff()
	bo.MaxElapsedTime = time.Duration(float64(c.config.OpenAI.Timeout) * 0.9)
	bo.InitialInterval = 1 * time.Second
	bo.MaxInterval = 10 * time.Second

	err := backoff.Retry(func() error {
		requestCtx, requestCancel := context.WithTimeout(timeoutCtx, c.config.OpenAI.Timeout)
		defer requestCancel()

		resp, err := c.doChat(requestCtx, messages, tools, imagePath)
		if err != nil {
			lastErr = err
			c.logger.Warn("LLM request failed, retrying",
				zap.Error(err),
				zap.Duration("timeout", c.config.OpenAI.Timeout),
			)
			return err
		}
		result = resp
		return nil
	}, backoff.WithContext(bo, timeoutCtx))

	if err != nil {
		return nil, fmt.Errorf("LLM request failed after retries: %w", lastErr)
	}

	return result, nil
}

func (c *OpenAIClient) doChat(ctx context.Context, messages []Message, tools []Tool, imagePath string) (*ChatResponse, error) {
	openAIMessages := make([]openai.ChatCompletionMessage, 0, len(messages))

	for _, msg := range messages {
		openAIMsg := openai.ChatCompletionMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}

		if imagePath != "" && msg.Role == openai.ChatMessageRoleUser && msg == messages[len(messages)-1] {
			imageData, err := os.ReadFile(imagePath)
			if err != nil {
				c.logger.Warn("Failed to read image file", zap.String("path", imagePath), zap.Error(err))
				openAIMsg.Content = msg.Content
			} else {
				imageBase64 := base64.StdEncoding.EncodeToString(imageData)
				openAIMsg.MultiContent = []openai.ChatMessagePart{
					{
						Type: openai.ChatMessagePartTypeText,
						Text: msg.Content,
					},
					{
						Type: openai.ChatMessagePartTypeImageURL,
						ImageURL: &openai.ChatMessageImageURL{
							URL: fmt.Sprintf("data:image/png;base64,%s", imageBase64),
						},
					},
				}
				openAIMsg.Content = "" // очищаем, т.к. используем MultiContent
			}
		}

		openAIMessages = append(openAIMessages, openAIMsg)
	}

	req := openai.ChatCompletionRequest{
		Model:    c.model,
		Messages: openAIMessages,
	}

	if len(tools) > 0 {
		openAITools := make([]openai.Tool, 0, len(tools))
		for _, tool := range tools {
			openAITool := openai.Tool{
				Type: openai.ToolTypeFunction,
				Function: &openai.FunctionDefinition{
					Name:        tool.Name,
					Description: tool.Description,
				},
			}
			if tool.Parameters != nil {
				paramsJSON, _ := json.Marshal(tool.Parameters)
				openAITool.Function.Parameters = string(paramsJSON)
			}
			openAITools = append(openAITools, openAITool)
		}
		req.Tools = openAITools
	}

	c.logger.Debug("llm_request",
		zap.String("type", "llm_request"),
		zap.String("model", c.model),
		zap.Int("messages", len(messages)),
		zap.Int("tools", len(tools)),
	)

	resp, err := c.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("openai API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("empty response from LLM")
	}

	choice := resp.Choices[0]
	result := &ChatResponse{
		Content:      choice.Message.Content,
		ToolCalls:    []ToolCall{},
		FinishReason: string(choice.FinishReason),
	}

	c.logger.Info("LLM response received",
		zap.String("model", c.model),
		zap.String("finish_reason", string(choice.FinishReason)),
		zap.Int("tool_calls_count", len(choice.Message.ToolCalls)),
		zap.String("content", func() string {
			content := choice.Message.Content
			if len(content) > 500 {
				return content[:500] + "... (truncated)"
			}
			return content
		}()),
	)

	if len(choice.Message.ToolCalls) > 0 {
		for _, tc := range choice.Message.ToolCalls {
			args := make(map[string]interface{})
			if tc.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
					c.logger.Warn("Failed to parse tool call arguments", zap.Error(err), zap.String("arguments", tc.Function.Arguments))
				}
			}

			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:   tc.ID,
				Name: tc.Function.Name,
				Args: args,
			})

			c.logger.Debug("Tool call extracted",
				zap.String("id", tc.ID),
				zap.String("name", tc.Function.Name),
				zap.Any("args", args),
			)
		}
	}

	c.logger.Debug("llm_response",
		zap.String("type", "llm_response"),
		zap.Int("content_len", len(result.Content)),
		zap.Int("tool_calls", len(result.ToolCalls)),
		zap.String("finish_reason", result.FinishReason),
	)

	return result, nil
}
