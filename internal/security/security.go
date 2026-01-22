package security

import (
	"encoding/json"
	"fmt"
	"strings"
)

type SecurityChecker struct {
	dangerousKeywords []string
}

type ApprovalRequest struct {
	Action string      `json:"action"`
	Tool   string      `json:"tool"`
	Target interface{} `json:"target,omitempty"`
	Args   interface{} `json:"args,omitempty"`
	Reason string      `json:"reason"`
}

func New() *SecurityChecker {
	return &SecurityChecker{
		dangerousKeywords: []string{
			"оплата", "payment", "pay", "купить", "buy", "purchase",
			"оформить заказ", "order", "checkout",
			"отправить", "submit", "send",
			"удалить", "delete", "remove",
			"откликнуться", "apply", "отклик",
		},
	}
}

func (s *SecurityChecker) CheckAction(tool string, args map[string]interface{}) (bool, *ApprovalRequest) {
	if tool == "type" {
		if text, ok := args["text"].(string); ok {
			if s.containsDangerousKeyword(text) {
				return false, &ApprovalRequest{
					Action: "type",
					Tool:   tool,
					Args:   args,
					Reason: fmt.Sprintf("Potentially dangerous text: %s", text),
				}
			}
		}
	}

	if tool == "click" {
		targetJSON, _ := json.Marshal(args["target"])
		targetStr := string(targetJSON)
		if s.containsDangerousKeyword(targetStr) {
			return false, &ApprovalRequest{
				Action: "click",
				Tool:   tool,
				Target: args["target"],
				Reason: fmt.Sprintf("Potentially dangerous click on element with dangerous keywords"),
			}
		}
	}

	if tool == "navigate" {
		if url, ok := args["url"].(string); ok {
			if s.containsDangerousKeyword(url) {
				return false, &ApprovalRequest{
					Action: "navigate",
					Tool:   tool,
					Args:   args,
					Reason: fmt.Sprintf("Potentially dangerous URL: %s", url),
				}
			}
		}
	}

	return true, nil
}

func (s *SecurityChecker) containsDangerousKeyword(text string) bool {
	lower := strings.ToLower(text)
	for _, keyword := range s.dangerousKeywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

func (s *SecurityChecker) IsDangerousTool(tool string) bool {
	dangerousTools := []string{"click", "type", "submit", "navigate"}
	for _, dt := range dangerousTools {
		if tool == dt {
			return true
		}
	}
	return false
}
