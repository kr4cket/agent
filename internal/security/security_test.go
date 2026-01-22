package security

import (
	"testing"
)

func TestSecurityChecker_CheckAction(t *testing.T) {
	checker := New()

	tests := []struct {
		name     string
		tool     string
		args     map[string]interface{}
		approved bool
	}{
		{
			name:     "safe navigate",
			tool:     "navigate",
			args:     map[string]interface{}{"url": "https://example.com"},
			approved: true,
		},
		{
			name:     "dangerous type (payment)",
			tool:     "type",
			args:     map[string]interface{}{"text": "оплата"},
			approved: false,
		},
		{
			name:     "dangerous click (order)",
			tool:     "click",
			args:     map[string]interface{}{"target": map[string]interface{}{"text_contains": "оформить заказ"}},
			approved: false,
		},
		{
			name:     "safe click",
			tool:     "click",
			args:     map[string]interface{}{"target": map[string]interface{}{"role": "button"}},
			approved: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			approved, _ := checker.CheckAction(tt.tool, tt.args)
			if approved != tt.approved {
				t.Errorf("Expected approved=%v, got %v", tt.approved, approved)
			}
		})
	}
}

func TestSecurityChecker_IsDangerousTool(t *testing.T) {
	checker := New()

	tests := []struct {
		tool      string
		dangerous bool
	}{
		{"click", true},
		{"type", true},
		{"navigate", true},
		{"observe", false},
		{"screenshot", false},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			dangerous := checker.IsDangerousTool(tt.tool)
			if dangerous != tt.dangerous {
				t.Errorf("Expected dangerous=%v for tool %s, got %v", tt.dangerous, tt.tool, dangerous)
			}
		})
	}
}
