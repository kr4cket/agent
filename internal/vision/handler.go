package vision

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	"testTask/internal/browser"
	"testTask/internal/domvalidation"
	"testTask/internal/logging"
	"testTask/internal/memory"
	"testTask/internal/subagents"
)

type VisionActionHandler struct {
	browser        browser.Browser
	analyst        subagents.AnalystInterface
	domValidator   *domvalidation.Validator
	viewportWidth  int
	viewportHeight int
	logger         *zap.Logger
}

func NewVisionActionHandler(
	browser browser.Browser,
	analyst subagents.AnalystInterface,
	domValidator *domvalidation.Validator,
	viewportWidth, viewportHeight int,
	logger *logging.Logger,
) *VisionActionHandler {
	return &VisionActionHandler{
		browser:        browser,
		analyst:        analyst,
		domValidator:   domValidator,
		viewportWidth:  viewportWidth,
		viewportHeight: viewportHeight,
		logger:         logger.Console(),
	}
}

func (h *VisionActionHandler) HandleAction(
	ctx context.Context,
	toolName string,
	args map[string]interface{},
	target *memory.Target,
	screenshotPath string,
	description string,
) (interface{}, bool, error) {
	if (toolName != "click" && toolName != "type") || screenshotPath == "" || target == nil {
		return nil, false, nil
	}

	h.logger.Info("Attempting vision-based element location",
		zap.String("tool", toolName),
		zap.String("screenshot", screenshotPath),
		zap.Any("target", target),
	)

	url, _ := h.browser.GetURL(ctx)
	title, _ := h.browser.GetTitle(ctx)
	pageContext := fmt.Sprintf("Current page: %s (Title: %s)", url, title)

	coords, err := h.analyst.FindClickCoordinates(ctx, screenshotPath, target, description, pageContext)
	if err != nil {
		h.logger.Warn("Vision analysis failed, using selector fallback", zap.Error(err))
		return nil, false, nil
	}

	if coords == nil || !coords.Found {
		reason := ""
		if coords != nil {
			reason = coords.Reason
		}
		h.logger.Warn("Vision analysis did not find element, using selector fallback",
			zap.String("reason", reason),
		)
		return nil, false, nil
	}

	if coords.X < 0 || coords.Y < 0 ||
		coords.X > h.viewportWidth ||
		coords.Y > h.viewportHeight {
		h.logger.Warn("Vision coordinates are outside viewport, using selector fallback",
			zap.Int("x", coords.X),
			zap.Int("y", coords.Y),
			zap.Int("viewport_width", h.viewportWidth),
			zap.Int("viewport_height", h.viewportHeight),
		)
		return nil, false, nil
	}

	h.logger.Info("Vision analysis found element coordinates",
		zap.Int("x", coords.X),
		zap.Int("y", coords.Y),
	)

	validated, domErr := h.domValidator.Validate(ctx, coords.X, coords.Y, target, coords.ElementText)
	if domErr != nil {
		h.logger.Warn("DOM validation failed, falling back to selector",
			zap.Error(domErr),
			zap.Int("x", coords.X),
			zap.Int("y", coords.Y),
			zap.String("llm_element_text", coords.ElementText),
		)
		return nil, false, nil
	}

	if !validated {
		h.logger.Warn("DOM validation suggests coordinates may be incorrect, falling back to selector",
			zap.Int("x", coords.X),
			zap.Int("y", coords.Y),
			zap.String("llm_element_text", coords.ElementText),
		)
		return nil, false, nil
	}

	h.logger.Info("Coordinates validated through DOM analysis",
		zap.Int("x", coords.X),
		zap.Int("y", coords.Y),
		zap.String("llm_element_text", coords.ElementText),
	)

	if toolName == "click" {
		if err := h.browser.ClickAt(ctx, coords.X, coords.Y); err != nil {
			h.logger.Warn("Vision-based click failed, falling back to selector", zap.Error(err))
			return nil, false, nil
		}

		time.Sleep(500 * time.Millisecond)
		h.logger.Info("Vision-based click executed successfully",
			zap.Int("x", coords.X),
			zap.Int("y", coords.Y),
		)
		return map[string]interface{}{"method": "vision", "x": coords.X, "y": coords.Y}, true, nil

	} else if toolName == "type" {
		text, _ := args["text"].(string)
		if text == "" {
			return nil, false, fmt.Errorf("text is required for type action")
		}

		if err := h.browser.TypeAt(ctx, coords.X, coords.Y, text); err != nil {
			h.logger.Warn("Vision-based type failed, falling back to selector", zap.Error(err))
			return nil, false, nil
		}

		time.Sleep(300 * time.Millisecond)
		h.logger.Info("Vision-based type executed successfully",
			zap.Int("x", coords.X),
			zap.Int("y", coords.Y),
			zap.String("text", text),
		)
		return map[string]interface{}{"method": "vision", "x": coords.X, "y": coords.Y}, true, nil
	}

	return nil, false, nil
}
