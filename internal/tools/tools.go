package tools

import (
	"context"
	"fmt"

	"testTask/internal/llm"
	"testTask/internal/memory"
)

type ToolRegistry struct {
	tools map[string]ToolExecutor
}

type ToolExecutor interface {
	Execute(ctx context.Context, args map[string]interface{}) (interface{}, error)
	Validate(args map[string]interface{}) error
}

func NewRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]ToolExecutor),
	}
}

func (r *ToolRegistry) Register(name string, executor ToolExecutor) {
	r.tools[name] = executor
}

func (r *ToolRegistry) GetExecutor(name string) (ToolExecutor, bool) {
	executor, ok := r.tools[name]
	return executor, ok
}

func (r *ToolRegistry) GetToolsDefinitions() []llm.Tool {
	tools := make([]llm.Tool, 0, len(r.tools))

	for name := range r.tools {
		tool := llm.Tool{
			Type:        "function",
			Name:        name,
			Description: getToolDescription(name),
			Parameters:  getToolParameters(name),
		}
		tools = append(tools, tool)
	}

	return tools
}

func getToolDescription(name string) string {
	descriptions := map[string]string{
		"navigate":   "Navigate to a URL",
		"observe":    "Observe the current page state. Can be focused (with query) or general",
		"click":      "Click on an element identified by a structural Target (not CSS/XPath)",
		"type":       "Type text into an input element identified by a structural Target",
		"press":      "Press a keyboard key",
		"scroll":     "Scroll the page (direction: up/down, amount: pixels)",
		"wait":       "Wait for a condition or time",
		"back":       "Navigate back in browser history",
		"screenshot": "Take a screenshot of the current page",
		"set_mode":   "Set agent mode (live/dryrun)",
	}

	return descriptions[name]
}

func getToolParameters(name string) map[string]interface{} {
	baseTargetSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"role": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"button", "link", "textbox", "combobox", "checkbox", "menuitem", "tab", "item", "generic"},
				"description": "Element role",
			},
			"name": map[string]interface{}{
				"type":        "string",
				"description": "Element name attribute",
			},
			"text_contains": map[string]interface{}{
				"type":        "string",
				"description": "Text content contains",
			},
			"label": map[string]interface{}{
				"type":        "string",
				"description": "Aria-label or label text",
			},
			"placeholder": map[string]interface{}{
				"type":        "string",
				"description": "Placeholder text",
			},
			"nth": map[string]interface{}{
				"type":        "integer",
				"description": "N-th occurrence (0-indexed)",
				"default":     0,
			},
			"hints": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Additional hints",
			},
			"nearby_text": map[string]interface{}{
				"type":        "string",
				"description": "Text nearby the element",
			},
		},
	}

	params := map[string]interface{}{
		"navigate": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "URL to navigate to",
				},
			},
			"required": []string{"url"},
		},
		"observe": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"focused": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to focus on specific query",
				},
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Focus query for focused observation",
				},
			},
		},
		"click": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"target": baseTargetSchema,
			},
			"required": []string{"target"},
		},
		"type": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"target": baseTargetSchema,
				"text": map[string]interface{}{
					"type":        "string",
					"description": "Text to type",
				},
			},
			"required": []string{"target", "text"},
		},
		"press": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key": map[string]interface{}{
					"type":        "string",
					"description": "Key to press (e.g., Enter, Escape, Tab)",
				},
			},
			"required": []string{"key"},
		},
		"scroll": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"direction": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"up", "down"},
					"description": "Scroll direction",
				},
				"amount": map[string]interface{}{
					"type":        "integer",
					"description": "Scroll amount in pixels",
					"default":     500,
				},
			},
			"required": []string{"direction"},
		},
		"wait": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"condition": map[string]interface{}{
					"type":        "string",
					"description": "Condition to wait for",
				},
				"timeout": map[string]interface{}{
					"type":        "integer",
					"description": "Timeout in milliseconds",
					"default":     3000,
				},
			},
		},
		"back": map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		"screenshot": map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		"set_mode": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"mode": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"live", "dryrun"},
					"description": "Agent mode",
				},
			},
			"required": []string{"mode"},
		},
	}

	if p, ok := params[name]; ok {
		return p.(map[string]interface{})
	}

	return map[string]interface{}{}
}

func ValidateTarget(target map[string]interface{}) error {
	if role, ok := target["role"].(string); ok {
		validRoles := []string{"button", "link", "textbox", "combobox", "checkbox", "menuitem", "tab", "item", "generic"}
		valid := false
		for _, vr := range validRoles {
			if role == vr {
				valid = true
				break
			}
		}
		if !valid && role != "" {
			return fmt.Errorf("invalid role: %s, must be one of: %v", role, validRoles)
		}
	}

	if _, ok := target["selector"].(string); ok {
		return fmt.Errorf("CSS/XPath selectors are not allowed, use structural Target")
	}
	if _, ok := target["xpath"].(string); ok {
		return fmt.Errorf("CSS/XPath selectors are not allowed, use structural Target")
	}
	if _, ok := target["css"].(string); ok {
		return fmt.Errorf("CSS/XPath selectors are not allowed, use structural Target")
	}

	return nil
}

func ParseTarget(args map[string]interface{}) (*memory.Target, error) {
	targetData, ok := args["target"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("target is required and must be an object")
	}

	if err := ValidateTarget(targetData); err != nil {
		return nil, err
	}

	target := &memory.Target{}

	if role, ok := targetData["role"].(string); ok {
		target.Role = role
	}
	if name, ok := targetData["name"].(string); ok {
		target.Name = name
	}
	if textContains, ok := targetData["text_contains"].(string); ok {
		target.TextContains = textContains
	}
	if label, ok := targetData["label"].(string); ok {
		target.Label = label
	}
	if placeholder, ok := targetData["placeholder"].(string); ok {
		target.Placeholder = placeholder
	}
	if nth, ok := targetData["nth"].(float64); ok {
		target.Nth = int(nth)
	}
	if hints, ok := targetData["hints"].([]interface{}); ok {
		target.Hints = make([]string, 0, len(hints))
		for _, h := range hints {
			if s, ok := h.(string); ok {
				target.Hints = append(target.Hints, s)
			}
		}
	}
	if nearbyText, ok := targetData["nearby_text"].(string); ok {
		target.NearbyText = nearbyText
	}

	return target, nil
}
