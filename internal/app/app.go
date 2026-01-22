package app

import (
	"context"
	"fmt"

	"testTask/internal/agent"
	"testTask/internal/browser"
	"testTask/internal/config"
	"testTask/internal/domvalidation"
	"testTask/internal/llm"
	"testTask/internal/logging"
	"testTask/internal/memory"
	"testTask/internal/security"
	"testTask/internal/subagents"
	"testTask/internal/tools"
	"testTask/internal/vision"
)

type App struct {
	config       *config.Config
	logger       *logging.Logger
	browser      browser.Browser
	llm          llm.LLMClient
	agent        *agent.Agent
	toolRegistry *tools.ToolRegistry
}

func New(cfg *config.Config) (*App, error) {
	logger, err := logging.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize logging: %w", err)
	}

	br, err := browser.New(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize browser: %w", err)
	}

	llmClient := llm.New(cfg, logger)

	planner := subagents.NewPlanner(llmClient, logger)
	analyst := subagents.NewDOMAnalyst(llmClient, logger)
	critic := subagents.NewCritic(llmClient, logger)

	mem := memory.New(cfg.Agent.EphemeralSize, cfg.Agent.WorkingSummarySize)

	sec := security.New()

	registry := tools.NewRegistry()

	screensDir := logger.ScreensDir()
	dryRun := cfg.Agent.DryRun

	registry.Register("navigate", tools.NewNavigateExecutor(br, logger))
	registry.Register("observe", tools.NewObserveExecutor(br, logger))
	registry.Register("click", tools.NewClickExecutor(br, logger, dryRun))
	registry.Register("type", tools.NewTypeExecutor(br, logger, dryRun))
	registry.Register("press", tools.NewPressExecutor(br, logger, dryRun))
	registry.Register("scroll", tools.NewScrollExecutor(br, logger, dryRun))
	registry.Register("wait", tools.NewWaitExecutor(br, logger, dryRun))
	registry.Register("back", tools.NewBackExecutor(br, logger, dryRun))
	registry.Register("screenshot", tools.NewScreenshotExecutor(br, logger, screensDir, dryRun))

	domValidator := domvalidation.NewValidator(br, logger.Console())
	visionHandler := vision.NewVisionActionHandler(
		br,
		analyst,
		domValidator,
		cfg.Browser.ViewportWidth,
		cfg.Browser.ViewportHeight,
		logger,
	)

	agentOptions := agent.NewAgentOptions(
		cfg.Agent.MaxSteps,
		cfg.Agent.CriticInterval,
		cfg.Agent.DryRun,
		cfg.Browser.ViewportWidth,
		cfg.Browser.ViewportHeight,
	)

	ag := agent.New(
		br,
		llmClient,
		registry,
		sec,
		planner,
		analyst,
		critic,
		mem,
		logger,
		agentOptions,
		domValidator,
		visionHandler,
	)

	setModeExecutor := tools.NewSetModeExecutor(logger, func(dryRun bool) {
		ag.SetMode(dryRun)
	})
	registry.Register("set_mode", setModeExecutor)

	return &App{
		config:       cfg,
		logger:       logger,
		browser:      br,
		llm:          llmClient,
		agent:        ag,
		toolRegistry: registry,
	}, nil
}

func (a *App) GetAgent() *agent.Agent {
	return a.agent
}

func (a *App) GetLogger() *logging.Logger {
	return a.logger
}

func (a *App) GetBrowser() browser.Browser {
	return a.browser
}

func (a *App) Close(ctx context.Context) error {
	var errs []error

	if a.browser != nil {
		if err := a.browser.Close(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	if a.logger != nil {
		if err := a.logger.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors during close: %v", errs)
	}

	return nil
}
