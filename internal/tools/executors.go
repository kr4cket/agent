package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"go.uber.org/zap"
	"testTask/internal/browser"
	"testTask/internal/logging"
)

type NavigateExecutor struct {
	browser browser.Browser
	logger  *zap.Logger
}

func NewNavigateExecutor(browser browser.Browser, logger *logging.Logger) *NavigateExecutor {
	return &NavigateExecutor{
		browser: browser,
		logger:  logger.Console(),
	}
}

func (e *NavigateExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	url, ok := args["url"].(string)
	if !ok {
		return nil, fmt.Errorf("url is required")
	}

	if err := e.browser.Navigate(ctx, url); err != nil {
		return nil, fmt.Errorf("navigate failed: %w", err)
	}

	return map[string]interface{}{"url": url}, nil
}

func (e *NavigateExecutor) Validate(args map[string]interface{}) error {
	if _, ok := args["url"].(string); !ok {
		return fmt.Errorf("url is required")
	}
	return nil
}

type ObserveExecutor struct {
	browser browser.Browser
	logger  *zap.Logger
}

func NewObserveExecutor(browser browser.Browser, logger *logging.Logger) *ObserveExecutor {
	return &ObserveExecutor{
		browser: browser,
		logger:  logger.Console(),
	}
}

func (e *ObserveExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	focused, _ := args["focused"].(bool)
	query, _ := args["query"].(string)

	state, err := e.browser.Observe(ctx, focused, query)
	if err != nil {
		return nil, fmt.Errorf("observe failed: %w", err)
	}

	return state, nil
}

func (e *ObserveExecutor) Validate(args map[string]interface{}) error {
	return nil
}

type ClickExecutor struct {
	browser browser.Browser
	logger  *zap.Logger
	dryRun  bool
}

func NewClickExecutor(browser browser.Browser, logger *logging.Logger, dryRun bool) *ClickExecutor {
	return &ClickExecutor{
		browser: browser,
		logger:  logger.Console(),
		dryRun:  dryRun,
	}
}

func (e *ClickExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	target, err := ParseTarget(args)
	if err != nil {
		return nil, fmt.Errorf("invalid target: %w", err)
	}

	if e.dryRun {
		e.logger.Info("WOULD CALL: click", zap.Any("target", target))
		return map[string]interface{}{"dry_run": true}, nil
	}

	if err := e.browser.Click(ctx, target); err != nil {
		return nil, fmt.Errorf("click failed: %w", err)
	}

	return map[string]interface{}{"clicked": true}, nil
}

func (e *ClickExecutor) Validate(args map[string]interface{}) error {
	_, err := ParseTarget(args)
	return err
}

type TypeExecutor struct {
	browser browser.Browser
	logger  *zap.Logger
	dryRun  bool
}

func NewTypeExecutor(browser browser.Browser, logger *logging.Logger, dryRun bool) *TypeExecutor {
	return &TypeExecutor{
		browser: browser,
		logger:  logger.Console(),
		dryRun:  dryRun,
	}
}

func (e *TypeExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	target, err := ParseTarget(args)
	if err != nil {
		return nil, fmt.Errorf("invalid target: %w", err)
	}

	text, ok := args["text"].(string)
	if !ok {
		return nil, fmt.Errorf("text is required")
	}

	if e.dryRun {
		e.logger.Info("WOULD CALL: type", zap.Any("target", target), zap.String("text", text))
		return map[string]interface{}{"dry_run": true}, nil
	}

	if err := e.browser.Type(ctx, target, text); err != nil {
		return nil, fmt.Errorf("type failed: %w", err)
	}

	return map[string]interface{}{"typed": true}, nil
}

func (e *TypeExecutor) Validate(args map[string]interface{}) error {
	if _, err := ParseTarget(args); err != nil {
		return err
	}
	if _, ok := args["text"].(string); !ok {
		return fmt.Errorf("text is required")
	}
	return nil
}

type PressExecutor struct {
	browser browser.Browser
	logger  *zap.Logger
	dryRun  bool
}

func NewPressExecutor(browser browser.Browser, logger *logging.Logger, dryRun bool) *PressExecutor {
	return &PressExecutor{
		browser: browser,
		logger:  logger.Console(),
		dryRun:  dryRun,
	}
}

func (e *PressExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	key, ok := args["key"].(string)
	if !ok {
		return nil, fmt.Errorf("key is required")
	}

	if e.dryRun {
		e.logger.Info("WOULD CALL: press", zap.String("key", key))
		return map[string]interface{}{"dry_run": true}, nil
	}

	if err := e.browser.Press(ctx, key); err != nil {
		return nil, fmt.Errorf("press failed: %w", err)
	}

	return map[string]interface{}{"pressed": true}, nil
}

func (e *PressExecutor) Validate(args map[string]interface{}) error {
	if _, ok := args["key"].(string); !ok {
		return fmt.Errorf("key is required")
	}
	return nil
}

type ScrollExecutor struct {
	browser browser.Browser
	logger  *zap.Logger
	dryRun  bool
}

func NewScrollExecutor(browser browser.Browser, logger *logging.Logger, dryRun bool) *ScrollExecutor {
	return &ScrollExecutor{
		browser: browser,
		logger:  logger.Console(),
		dryRun:  dryRun,
	}
}

func (e *ScrollExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	direction, ok := args["direction"].(string)
	if !ok {
		return nil, fmt.Errorf("direction is required")
	}

	amount := 500
	if a, ok := args["amount"].(float64); ok {
		amount = int(a)
	}

	if e.dryRun {
		e.logger.Info("WOULD CALL: scroll", zap.String("direction", direction), zap.Int("amount", amount))
		return map[string]interface{}{"dry_run": true}, nil
	}

	if err := e.browser.Scroll(ctx, direction, amount); err != nil {
		return nil, fmt.Errorf("scroll failed: %w", err)
	}

	return map[string]interface{}{"scrolled": true}, nil
}

func (e *ScrollExecutor) Validate(args map[string]interface{}) error {
	if _, ok := args["direction"].(string); !ok {
		return fmt.Errorf("direction is required")
	}
	return nil
}

type WaitExecutor struct {
	browser browser.Browser
	logger  *zap.Logger
	dryRun  bool
}

func NewWaitExecutor(browser browser.Browser, logger *logging.Logger, dryRun bool) *WaitExecutor {
	return &WaitExecutor{
		browser: browser,
		logger:  logger.Console(),
		dryRun:  dryRun,
	}
}

func (e *WaitExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	condition, _ := args["condition"].(string)

	timeoutMs := 2000
	if d, ok := args["duration"].(float64); ok {
		timeoutMs = int(d * 1000)
	} else if t, ok := args["timeout"].(float64); ok {
		timeoutMs = int(t)
	}
	if timeoutMs > 3000 {
		timeoutMs = 3000
	}
	if timeoutMs < 500 {
		timeoutMs = 500
	}

	if e.dryRun {
		e.logger.Info("WOULD CALL: wait", zap.String("condition", condition), zap.Int("timeout_ms", timeoutMs))
		return map[string]interface{}{"dry_run": true}, nil
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond
	if err := e.browser.Wait(ctx, condition, timeout); err != nil {
		return nil, fmt.Errorf("wait failed: %w", err)
	}

	return map[string]interface{}{"waited": true}, nil
}

func (e *WaitExecutor) Validate(args map[string]interface{}) error {
	return nil
}

type BackExecutor struct {
	browser browser.Browser
	logger  *zap.Logger
	dryRun  bool
}

func NewBackExecutor(browser browser.Browser, logger *logging.Logger, dryRun bool) *BackExecutor {
	return &BackExecutor{
		browser: browser,
		logger:  logger.Console(),
		dryRun:  dryRun,
	}
}

func (e *BackExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if e.dryRun {
		e.logger.Info("WOULD CALL: back")
		return map[string]interface{}{"dry_run": true}, nil
	}

	if err := e.browser.Back(ctx); err != nil {
		return nil, fmt.Errorf("back failed: %w", err)
	}

	return map[string]interface{}{"backed": true}, nil
}

func (e *BackExecutor) Validate(args map[string]interface{}) error {
	return nil
}

type ScreenshotExecutor struct {
	browser    browser.Browser
	logger     *zap.Logger
	screensDir string
	dryRun     bool
}

func NewScreenshotExecutor(browser browser.Browser, logger *logging.Logger, screensDir string, dryRun bool) *ScreenshotExecutor {
	return &ScreenshotExecutor{
		browser:    browser,
		logger:     logger.Console(),
		screensDir: screensDir,
		dryRun:     dryRun,
	}
}

func (e *ScreenshotExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if e.dryRun {
		e.logger.Info("WOULD CALL: screenshot")
		return map[string]interface{}{"dry_run": true}, nil
	}

	filename := fmt.Sprintf("step-%d.png", time.Now().UnixNano())
	path := filepath.Join(e.screensDir, filename)

	if err := e.browser.Screenshot(ctx, path); err != nil {
		return nil, fmt.Errorf("screenshot failed: %w", err)
	}

	return map[string]interface{}{"path": path}, nil
}

func (e *ScreenshotExecutor) Validate(args map[string]interface{}) error {
	return nil
}

type SetModeExecutor struct {
	logger  *zap.Logger
	setMode func(bool)
}

func NewSetModeExecutor(logger *logging.Logger, setMode func(bool)) *SetModeExecutor {
	return &SetModeExecutor{
		logger:  logger.Console(),
		setMode: setMode,
	}
}

func (e *SetModeExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	mode, ok := args["mode"].(string)
	if !ok {
		return nil, fmt.Errorf("mode is required")
	}

	dryRun := mode == "dryrun"
	e.setMode(dryRun)

	e.logger.Info("Mode changed", zap.String("mode", mode))

	return map[string]interface{}{"mode": mode}, nil
}

func (e *SetModeExecutor) Validate(args map[string]interface{}) error {
	if mode, ok := args["mode"].(string); ok {
		if mode != "live" && mode != "dryrun" {
			return fmt.Errorf("mode must be 'live' or 'dryrun'")
		}
	} else {
		return fmt.Errorf("mode is required")
	}
	return nil
}
