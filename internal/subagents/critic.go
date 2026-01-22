package subagents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go.uber.org/zap"
	"testTask/internal/llm"
	"testTask/internal/logging"
	"testTask/internal/memory"
)

type Critic struct {
	llm    llm.LLMClient
	logger *zap.Logger
}

type Critique struct {
	OnTrack         bool     `json:"on_track"`
	Progress        float64  `json:"progress"` // 0.0 to 1.0
	Issues          []string `json:"issues,omitempty"`
	Recommendations []string `json:"recommendations,omitempty"`
	Blocked         bool     `json:"blocked"`
	BlockReason     string   `json:"block_reason,omitempty"`
}

func NewCritic(llm llm.LLMClient, logger *logging.Logger) *Critic {
	return &Critic{
		llm:    llm,
		logger: logger.Console(),
	}
}

func (c *Critic) Critique(ctx context.Context, goal string, plan *Plan, history []memory.Step, currentState *memory.PageState) (*Critique, error) {
	prompt := c.buildPrompt(goal, plan, history, currentState)

	messages := []llm.Message{
		{
			Role:    "system",
			Content: c.getSystemPrompt(),
		},
		{
			Role:    "user",
			Content: prompt,
		},
	}

	resp, err := c.llm.Chat(ctx, messages, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to critique: %w", err)
	}

	c.logger.Info("LLM critic response",
		zap.String("full_response", resp.Content),
		zap.String("finish_reason", resp.FinishReason),
	)

	critique, err := c.parseCritique(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse critique: %w", err)
	}

	c.logger.Info("Critique completed", zap.Any("critique", critique))

	return critique, nil
}

func (c *Critic) getSystemPrompt() string {
	return `You are a Critic subagent. Your task is to evaluate the agent's progress towards the goal and identify issues.

Return your response as a JSON object with the following structure:
{
  "on_track": true/false,
  "progress": 0.0-1.0,
  "issues": ["issue1", "issue2"],
  "recommendations": ["recommendation1", "recommendation2"],
  "blocked": true/false,
  "block_reason": "reason if blocked"
}

Focus on:
- Whether the agent is making progress
- Any errors or unexpected behaviors
- Missing steps in the plan
- Recommendations for improvement
- Whether the agent is blocked and why`
}

func (c *Critic) buildPrompt(goal string, plan *Plan, history []memory.Step, state *memory.PageState) string {
	prompt := fmt.Sprintf("Goal: %s\n\n", goal)

	if plan != nil {
		prompt += fmt.Sprintf("Plan:\n- Steps: %d\n- Current step: %d\n- Blocked: %v\n",
			len(plan.Steps), plan.CurrentStep, plan.Blocked)
		if plan.Blocked {
			prompt += fmt.Sprintf("- Block reason: %s\n", plan.BlockReason)
		}
	}

	if len(history) > 0 {
		prompt += fmt.Sprintf("\nRecent steps (%d):\n", len(history))
		for i, step := range history {
			if i >= 10 {
				prompt += fmt.Sprintf("... and %d more steps\n", len(history)-10)
				break
			}
			prompt += fmt.Sprintf("- Step %d: %s (tool: %s)", i+1, step.Action, step.Tool)
			if step.Error != "" {
				prompt += fmt.Sprintf(" [ERROR: %s]", step.Error)
			}
			prompt += "\n"
		}
	}

	if state != nil {
		prompt += fmt.Sprintf("\nCurrent state:\n- URL: %s\n- Title: %s\n", state.URL, state.Title)
		if len(state.Overlays) > 0 {
			prompt += "- Overlays detected\n"
		}
	}

	prompt += "\nEvaluate the progress and identify any issues."

	return prompt
}

func (c *Critic) parseCritique(content string) (*Critique, error) {
	var critique Critique

	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no JSON found in response")
	}

	jsonStr := content[start : end+1]
	if err := json.Unmarshal([]byte(jsonStr), &critique); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &critique, nil
}
