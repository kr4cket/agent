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

type Planner struct {
	llm    llm.LLMClient
	logger *zap.Logger
}

type Plan struct {
	Goal        string `json:"goal"`
	Steps       []Step `json:"steps"`
	CurrentStep int    `json:"current_step"`
	Blocked     bool   `json:"blocked"`
	BlockReason string `json:"block_reason,omitempty"`
}

type Step struct {
	ID          string      `json:"id"`
	Description string      `json:"description"`
	Tool        string      `json:"tool,omitempty"`
	Target      interface{} `json:"target,omitempty"`
	Args        interface{} `json:"args,omitempty"`
}

func NewPlanner(llm llm.LLMClient, logger *logging.Logger) *Planner {
	return &Planner{
		llm:    llm,
		logger: logger.Console(),
	}
}

func (p *Planner) CreatePlan(ctx context.Context, task string, currentState *memory.PageState, memoryContext string) (*Plan, error) {
	prompt := p.buildPrompt(task, currentState, memoryContext)

	messages := []llm.Message{
		{
			Role:    "system",
			Content: p.getSystemPrompt(),
		},
		{
			Role:    "user",
			Content: prompt,
		},
	}

	resp, err := p.llm.Chat(ctx, messages, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create plan: %w", err)
	}

	p.logger.Info("LLM planner response",
		zap.String("task", task),
		zap.String("full_response", resp.Content),
		zap.String("finish_reason", resp.FinishReason),
	)

	plan, err := p.parsePlan(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse plan: %w", err)
	}

	p.logger.Info("Plan created", zap.Any("plan", plan))

	return plan, nil
}

func (p *Planner) UpdatePlan(ctx context.Context, currentPlan *Plan, currentState *memory.PageState, errorMsg string) (*Plan, error) {
	prompt := fmt.Sprintf(`Current plan: %s
Current state URL: %s
Error: %s

Update the plan to handle the error. Return updated plan in JSON format.`,
		p.planToJSON(currentPlan), currentState.URL, errorMsg)

	messages := []llm.Message{
		{
			Role:    "system",
			Content: p.getSystemPrompt(),
		},
		{
			Role:    "user",
			Content: prompt,
		},
	}

	resp, err := p.llm.Chat(ctx, messages, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to update plan: %w", err)
	}

	p.logger.Info("LLM planner update response",
		zap.String("error_context", errorMsg),
		zap.String("full_response", resp.Content),
		zap.String("finish_reason", resp.FinishReason),
	)

	plan, err := p.parsePlan(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse updated plan: %w", err)
	}

	return plan, nil
}

func (p *Planner) getSystemPrompt() string {
	return `You are a Planner subagent. Your task is to create a step-by-step plan for completing a user task in a web browser.

Return your response as a JSON object with the following structure:
{
  "goal": "description of the goal",
  "steps": [
    {
      "id": "step-1",
      "description": "description of the step",
      "tool": "navigate|observe|click|type|press|scroll|wait|back|screenshot",
      "target": {...}, // only if tool requires target
      "args": {...}    // other tool arguments
    }
  ],
  "current_step": 0,
  "blocked": false
}

IMPORTANT:
- Do NOT use CSS/XPath selectors. Use structural Target objects only.
- Target structure: {"role": "button|link|textbox|...", "name": "...", "text_contains": "...", "label": "...", "placeholder": "...", "nth": 0}
- Break down complex tasks into small, actionable steps.
- Always start with "observe" to understand the current page state.
- Be specific about what elements to interact with using structural descriptions.
- CRITICAL: The "navigate" tool is ONLY for navigating to URLs. It requires "args": {"url": "https://..."}.
- Do NOT use "navigate" to find elements on a page - use "observe" for that.
- If you need to find an element, use "observe" with a query describing what to look for.
- SCROLLING: The page can be scrolled. If an element is not found on the current view (e.g. search box, button, link), add a "scroll" step (tool: scroll, args: {direction: "up"|"down", amount: pixels}) before retrying. Use scroll to reveal content below or above the fold, then observe/click/type again.`
}

func (p *Planner) buildPrompt(task string, state *memory.PageState, memoryContext string) string {
	var prompt string

	prompt += fmt.Sprintf("Task: %s\n\n", task)

	if memoryContext != "" {
		prompt += fmt.Sprintf("Context:\n%s\n\n", memoryContext)
	}

	if state != nil {
		prompt += fmt.Sprintf("Current page:\n- URL: %s\n- Title: %s\n", state.URL, state.Title)
		if len(state.Elements) > 0 {
			prompt += fmt.Sprintf("- Available elements: %d\n", len(state.Elements))
		}
	}

	prompt += "\nCreate a plan to complete this task."

	return prompt
}

func (p *Planner) parsePlan(content string) (*Plan, error) {
	var plan Plan

	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no JSON found in response")
	}

	jsonStr := content[start : end+1]
	jsonStr = removeJSONComments(jsonStr)

	if err := json.Unmarshal([]byte(jsonStr), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &plan, nil
}

func (p *Planner) planToJSON(plan *Plan) string {
	data, _ := json.Marshal(plan)
	return string(data)
}
