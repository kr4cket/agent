package tools

import (
	"testing"
)

func TestValidateTarget(t *testing.T) {
	tests := []struct {
		name    string
		target  map[string]interface{}
		wantErr bool
	}{
		{
			name:    "valid target",
			target:  map[string]interface{}{"role": "button"},
			wantErr: false,
		},
		{
			name:    "invalid role",
			target:  map[string]interface{}{"role": "invalid"},
			wantErr: true,
		},
		{
			name:    "css selector rejected",
			target:  map[string]interface{}{"selector": ".button"},
			wantErr: true,
		},
		{
			name:    "xpath rejected",
			target:  map[string]interface{}{"xpath": "//button"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTarget(tt.target)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTarget() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseTarget(t *testing.T) {
	args := map[string]interface{}{
		"target": map[string]interface{}{
			"role":          "button",
			"name":          "submit",
			"text_contains": "Click",
			"label":         "Submit",
			"placeholder":   "",
			"nth":           0,
			"hints":         []interface{}{"primary", "submit"},
			"nearby_text":   "Submit form",
		},
	}

	target, err := ParseTarget(args)
	if err != nil {
		t.Fatalf("ParseTarget() error = %v", err)
	}

	if target.Role != "button" {
		t.Errorf("Expected role 'button', got '%s'", target.Role)
	}
	if target.Name != "submit" {
		t.Errorf("Expected name 'submit', got '%s'", target.Name)
	}
	if len(target.Hints) != 2 {
		t.Errorf("Expected 2 hints, got %d", len(target.Hints))
	}
}
