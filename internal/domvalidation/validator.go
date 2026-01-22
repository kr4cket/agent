package domvalidation

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"
	"testTask/internal/memory"
)

type Evaluator interface {
	Evaluate(ctx context.Context, script string, result interface{}) error
}

type Validator struct {
	eval Evaluator
	log  *zap.Logger
}

func NewValidator(eval Evaluator, log *zap.Logger) *Validator {
	return &Validator{eval: eval, log: log}
}

func (v *Validator) Validate(ctx context.Context, x, y int, target *memory.Target, llmElementText string) (bool, error) {
	if target == nil {
		return true, nil
	}

	jsCode := v.buildScript(x, y)
	var result map[string]interface{}
	if err := v.eval.Evaluate(ctx, jsCode, &result); err != nil {
		v.log.Warn("Failed to evaluate DOM at coordinates", zap.Error(err))
		return true, nil
	}

	if result == nil {
		v.log.Warn("DOM evaluation returned nil result", zap.Int("x", x), zap.Int("y", y))
		return false, fmt.Errorf("no result from DOM evaluation at coordinates (%d, %d)", x, y)
	}

	if found, ok := result["found"].(bool); !ok || !found {
		reason, _ := result["reason"].(string)
		v.log.Warn("No element found at coordinates", zap.Int("x", x), zap.Int("y", y), zap.String("reason", reason))
		return false, fmt.Errorf("no element found at coordinates (%d, %d): %s", x, y, reason)
	}

	v.checkRoleMatch(target, result, x, y)
	v.checkPlaceholder(target, result)
	v.checkLLMText(llmElementText, result)

	validationPassed, validationIssues, validationScore, maxScore := v.scoreValidation(target, result)
	v.checkVisibility(result, &validationPassed, &validationIssues)
	v.checkDisabled(result, &validationPassed, &validationIssues)
	v.logValidationResult(x, y, result, validationPassed, validationIssues, validationScore, maxScore)

	if target.Role == "textbox" {
		actualRole, _ := result["role"].(string)
		if actualRole != "textbox" {
			v.log.Warn("Textbox validation failed: role mismatch is critical",
				zap.String("expected", "textbox"), zap.String("actual", actualRole), zap.Int("x", x), zap.Int("y", y))
			return false, fmt.Errorf("textbox role mismatch: expected 'textbox', got '%s'", actualRole)
		}
	}

	if target.Role == "button" {
		actualRole, _ := result["role"].(string)
		if actualRole != "button" && actualRole != "link" && (actualRole == "generic" || actualRole == "") {
			v.log.Warn("Button validation failed: role is generic or empty",
				zap.String("expected", "button"), zap.String("actual", actualRole), zap.Int("x", x), zap.Int("y", y))
			return false, fmt.Errorf("button role mismatch: expected 'button', got '%s'", actualRole)
		}
	}

	return validationPassed, nil
}

func (v *Validator) buildScript(x, y int) string {
	return fmt.Sprintf(domAnalysisScriptTmpl, x, y)
}

func (v *Validator) checkRoleMatch(target *memory.Target, result map[string]interface{}, x, y int) {
	if target.Role == "" || target.Role == "generic" {
		return
	}
	actualRole, _ := result["role"].(string)
	if actualRole != target.Role {
		v.log.Warn("Role mismatch in DOM validation",
			zap.String("expected", target.Role), zap.String("actual", actualRole), zap.Int("x", x), zap.Int("y", y))
	}
}

func (v *Validator) checkPlaceholder(target *memory.Target, result map[string]interface{}) {
	if target.Placeholder == "" {
		return
	}
	actualPlaceholder, _ := result["placeholder"].(string)
	if actualPlaceholder != "" {
		expectedLower := strings.ToLower(target.Placeholder)
		actualLower := strings.ToLower(actualPlaceholder)
		if !strings.Contains(actualLower, expectedLower) && !strings.Contains(expectedLower, actualLower) {
			v.log.Debug("Placeholder mismatch in DOM validation",
				zap.String("expected", target.Placeholder), zap.String("actual", actualPlaceholder))
		}
	}
}

func (v *Validator) checkLLMText(llmElementText string, result map[string]interface{}) {
	if llmElementText == "" {
		return
	}
	actualText, _ := result["text"].(string)
	actualName, _ := result["name"].(string)
	actualPlaceholder, _ := result["placeholder"].(string)
	allText := strings.ToLower(actualText + " " + actualName + " " + actualPlaceholder)
	llmLower := strings.ToLower(llmElementText)

	if !strings.Contains(allText, llmLower) && !strings.Contains(llmLower, allText) {
		words := strings.Fields(llmLower)
		foundMatch := false
		for _, word := range words {
			if len(word) > 2 && strings.Contains(allText, word) {
				foundMatch = true
				break
			}
		}
		if !foundMatch {
			v.log.Warn("LLM element text does not match DOM element text",
				zap.String("llm_text", llmElementText), zap.String("dom_text", actualText),
				zap.String("dom_name", actualName), zap.String("dom_placeholder", actualPlaceholder))
		} else {
			v.log.Info("LLM element text partially matches DOM element", zap.String("llm_text", llmElementText), zap.String("dom_text", actualText))
		}
	} else {
		v.log.Info("LLM element text matches DOM element", zap.String("llm_text", llmElementText), zap.String("dom_text", actualText))
	}
}

func (v *Validator) scoreValidation(target *memory.Target, result map[string]interface{}) (passed bool, issues []string, score, maxScore int) {
	passed = true
	if target.Role != "" && target.Role != "generic" {
		maxScore++
		actualRole, _ := result["role"].(string)
		if actualRole == target.Role {
			score++
		} else {
			issues = append(issues, fmt.Sprintf("role mismatch: expected '%s', got '%s'", target.Role, actualRole))
			if target.Role == "textbox" && actualRole != "textbox" {
				passed = false
			} else if target.Role != "generic" && actualRole != "generic" && actualRole != "" {
				passed = false
			}
		}
	}
	if target.Placeholder != "" {
		maxScore++
		actualPlaceholder, _ := result["placeholder"].(string)
		if actualPlaceholder != "" {
			el, al := strings.ToLower(target.Placeholder), strings.ToLower(actualPlaceholder)
			if strings.Contains(al, el) || strings.Contains(el, al) {
				score++
			} else {
				issues = append(issues, fmt.Sprintf("placeholder mismatch: expected '%s', got '%s'", target.Placeholder, actualPlaceholder))
			}
		}
	}
	if target.TextContains != "" {
		maxScore++
		actualText, _ := result["text"].(string)
		actualName, _ := result["name"].(string)
		textToCheck := strings.ToLower(actualText + " " + actualName)
		if strings.Contains(textToCheck, strings.ToLower(target.TextContains)) {
			score++
		} else {
			issues = append(issues, fmt.Sprintf("text not found: expected '%s' in element text", target.TextContains))
		}
	}
	if target.Name != "" {
		maxScore++
		actualName, _ := result["name"].(string)
		el, al := strings.ToLower(target.Name), strings.ToLower(actualName)
		if strings.Contains(al, el) || strings.Contains(el, al) {
			score++
		} else {
			issues = append(issues, fmt.Sprintf("name mismatch: expected '%s', got '%s'", target.Name, actualName))
		}
	}
	if maxScore > 1 {
		ratio := float64(score) / float64(maxScore)
		if ratio < 0.5 && passed {
			v.log.Warn("Low validation score, but proceeding due to role match",
				zap.Float64("score_ratio", ratio), zap.Int("score", score), zap.Int("max_score", maxScore))
		}
	}
	return passed, issues, score, maxScore
}

func (v *Validator) checkVisibility(result map[string]interface{}, passed *bool, issues *[]string) {
	if vis, ok := result["isVisible"].(bool); ok && !vis {
		*issues = append(*issues, "element is not visible")
		*passed = false
	}
}

func (v *Validator) checkDisabled(result map[string]interface{}, passed *bool, issues *[]string) {
	if dis, ok := result["isDisabled"].(bool); ok && dis {
		*issues = append(*issues, "element is disabled")
		*passed = false
	}
}

func (v *Validator) logValidationResult(x, y int, result map[string]interface{}, passed bool, issues []string, score, maxScore int) {
	candidatesCount, _ := result["candidatesCount"].(float64)
	depth, _ := result["depth"].(float64)
	role, _ := result["role"].(string)
	ratio := float64(0)
	if maxScore > 0 {
		ratio = float64(score) / float64(maxScore)
	}
	if passed {
		v.log.Info("DOM validation passed (analyzed area around vision coordinates)",
			zap.Int("x", x), zap.Int("y", y), zap.String("role", role),
			zap.Float64("candidates_analyzed", candidatesCount), zap.Float64("element_depth", depth),
			zap.Float64("validation_score", ratio), zap.Int("score", score), zap.Int("max_score", maxScore))
	} else {
		v.log.Warn("DOM validation failed (analyzed area around vision coordinates)",
			zap.Int("x", x), zap.Int("y", y), zap.Strings("issues", issues),
			zap.Float64("candidates_analyzed", candidatesCount), zap.Float64("validation_score", ratio),
			zap.Int("score", score), zap.Int("max_score", maxScore))
	}
}
