package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"testTask/internal/browser"
	"testTask/internal/domvalidation"
	"testTask/internal/logging"
	"testTask/internal/memory"
	"testTask/internal/navigation"
	"testTask/internal/security"
	"testTask/internal/subagents"
	"testTask/internal/tools"
	"testTask/internal/vision"
)

type Agent struct {
	browser       browser.Browser
	toolRegistry  *tools.ToolRegistry
	security      *security.SecurityChecker
	planner       subagents.PlannerInterface
	analyst       subagents.AnalystInterface
	critic        subagents.CriticInterface
	mem           *memory.Memory
	logger        *logging.Logger
	options       AgentOptions
	domValidator  *domvalidation.Validator
	visionHandler *vision.VisionActionHandler

	currentPlan *subagents.Plan
	task        string
	dryRun      bool
	stepCount   int

	currentStepRetries int
	maxRetriesPerStep  int

	mu          sync.Mutex
	running     bool
	paused      bool
	stopped     bool
	approvalReq *security.ApprovalRequest
}

type AgentState string

const (
	StateIdle    AgentState = "idle"
	StateRunning AgentState = "running"
	StatePaused  AgentState = "paused"
	StateWaiting AgentState = "waiting_approval"
	StateError   AgentState = "error"
)

func New(
	browser browser.Browser,
	_ interface{},
	toolRegistry *tools.ToolRegistry,
	security *security.SecurityChecker,
	planner subagents.PlannerInterface,
	analyst subagents.AnalystInterface,
	critic subagents.CriticInterface,
	mem *memory.Memory,
	logger *logging.Logger,
	options AgentOptions,
	domValidator *domvalidation.Validator,
	visionHandler *vision.VisionActionHandler,
) *Agent {
	return &Agent{
		browser:            browser,
		toolRegistry:       toolRegistry,
		security:           security,
		planner:            planner,
		analyst:            analyst,
		critic:             critic,
		mem:                mem,
		logger:             logger,
		options:            options,
		domValidator:       domValidator,
		visionHandler:      visionHandler,
		dryRun:             options.DryRun,
		currentStepRetries: 0,
		maxRetriesPerStep:  5,
	}
}

func (a *Agent) Start(ctx context.Context, task string) error {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return fmt.Errorf("agent is already running")
	}
	a.running = true
	a.paused = false
	a.stopped = false
	a.task = task
	a.stepCount = 0
	a.mu.Unlock()

	a.logger.Console().Info("Starting agent", zap.String("task", task))

	url, err := a.browser.GetURL(ctx)
	if err == nil {
		if url == "about:blank" || url == "" {
			a.logger.Console().Info("Browser page is blank, agent will automatically navigate when needed", zap.String("current_url", url))
		} else {
			a.logger.Console().Info("Browser page is open", zap.String("current_url", url))
		}
	}

	a.mem.SetGoal(task)

	plan, err := a.planner.CreatePlan(ctx, task, nil, a.mem.GetContextForLLM())
	if err != nil {
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
		return fmt.Errorf("failed to create plan: %w", err)
	}

	url, err = a.browser.GetURL(ctx)
	if err == nil && (url == "about:blank" || url == "") {
		navigateURL := navigation.ResolveURLFromTask(task)
		if navigateURL != "" {
			hasNavigate := false
			if len(plan.Steps) > 0 && plan.Steps[0].Tool == "navigate" {
				hasNavigate = true
			}

			if !hasNavigate {
				a.logger.Console().Info("Adding navigation step to plan",
					zap.String("url", navigateURL),
					zap.String("reason", "page is blank"),
				)
				navigateStep := subagents.Step{
					ID:          "step-navigate-0",
					Description: fmt.Sprintf("Открыть сайт %s", navigateURL),
					Tool:        "navigate",
					Args: map[string]interface{}{
						"url": navigateURL,
					},
				}
				plan.Steps = append([]subagents.Step{navigateStep}, plan.Steps...)
				for i := 1; i < len(plan.Steps); i++ {
					plan.Steps[i].ID = fmt.Sprintf("step-%d", i)
				}
			}
		} else {
			a.logger.Console().Warn("Could not determine navigation URL from task, Planner should add navigate step",
				zap.String("task", task),
			)
		}
	}

	a.currentPlan = plan

	for !a.isStopped() {
		if a.isPaused() {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		if a.approvalReq != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		if err := a.executeNextStep(ctx); err != nil {
			a.logger.Console().Error("Step execution failed", zap.Error(err))

			a.mu.Lock()
			a.currentStepRetries++
			retries := a.currentStepRetries
			currentStep := a.currentPlan.CurrentStep
			a.mu.Unlock()

			a.logger.Console().Warn("Step retry",
				zap.Int("retry_count", retries),
				zap.Int("max_retries", a.maxRetriesPerStep),
				zap.Int("current_step", currentStep),
			)

			if retries >= a.maxRetriesPerStep {
				a.logger.Console().Warn("Max retries exceeded, rolling back to previous step",
					zap.Int("failed_step", currentStep),
					zap.Int("retries", retries),
				)

				if err := a.rollbackToPreviousStep(ctx); err != nil {
					a.logger.Console().Error("Rollback failed", zap.Error(err))
					if err := a.recoverFromError(ctx, err); err != nil {
						a.mu.Lock()
						a.running = false
						a.mu.Unlock()
						return fmt.Errorf("recovery failed: %w", err)
					}
				} else {
					continue
				}
			} else {
				if err := a.recoverFromError(ctx, err); err != nil {
					a.mu.Lock()
					a.running = false
					a.mu.Unlock()
					return fmt.Errorf("recovery failed: %w", err)
				}
			}
		} else {
			a.mu.Lock()
			a.currentStepRetries = 0
			a.mu.Unlock()
		}

		if a.stepCount%a.options.CriticInterval == 0 && a.stepCount > 0 {
			if err := a.checkWithCritic(ctx); err != nil {
				a.logger.Console().Warn("Critic check failed", zap.Error(err))
			}
		}

		if a.currentPlan.CurrentStep >= len(a.currentPlan.Steps) {
			a.logger.Console().Info("Plan completed")
			break
		}

		if a.stepCount >= a.options.MaxSteps {
			a.logger.Console().Warn("Max steps reached", zap.Int("max_steps", a.options.MaxSteps))
			break
		}
	}

	a.mu.Lock()
	a.running = false
	a.mu.Unlock()

	return nil
}

func (a *Agent) executeNextStep(ctx context.Context) error {
	if a.currentPlan == nil || a.currentPlan.CurrentStep >= len(a.currentPlan.Steps) {
		return fmt.Errorf("no more steps in plan")
	}

	step := a.currentPlan.Steps[a.currentPlan.CurrentStep]

	a.mu.Lock()
	retries := a.currentStepRetries
	a.mu.Unlock()

	a.logger.Console().Info("Executing step",
		zap.String("step_id", step.ID),
		zap.String("description", step.Description),
		zap.Int("attempt", retries+1),
		zap.Int("max_retries", a.maxRetriesPerStep),
	)

	isObserveStep := step.Tool == "observe"
	var state *memory.PageState
	screenshotPath := ""

	if !isObserveStep {
		var err error
		state, err = a.browser.Observe(ctx, false, "")
		if err != nil {
			return fmt.Errorf("observe failed: %w", err)
		}
		state = a.mem.TruncatePageState(state)
		a.mem.SetURL(state.URL)
		if !a.dryRun && (step.Tool == "click" || step.Tool == "type") {
			screensDir := a.logger.ScreensDir()
			filename := fmt.Sprintf("step-%d.png", a.stepCount)
			screenshotPath = fmt.Sprintf("%s/%s", screensDir, filename)
			if err := a.browser.Screenshot(ctx, screenshotPath); err != nil {
				a.logger.Console().Warn("Screenshot failed", zap.Error(err))
			}
		}
	}

	var result interface{}
	var err error
	if step.Tool != "" {
		result, err = a.executeTool(ctx, step.Tool, step.Args, step.Target, screenshotPath, step.Description)
		if err != nil {
			if isObserveStep {
				state, _ = a.browser.Observe(ctx, false, "")
				if state != nil {
					state = a.mem.TruncatePageState(state)
				} else {
					state = &memory.PageState{URL: "unknown", Timestamp: time.Now()}
				}
			}
			a.addStep(memory.Step{
				ID:        uuid.New().String(),
				Timestamp: time.Now(),
				Action:    step.Description,
				Tool:      step.Tool,
				Target:    step.Target,
				Error:     err.Error(),
				State:     state,
			})
			return fmt.Errorf("tool execution failed: %w", err)
		}
		if isObserveStep {
			if ps, ok := result.(*memory.PageState); ok {
				state = a.mem.TruncatePageState(ps)
				a.mem.SetURL(state.URL)
			} else {
				state = &memory.PageState{URL: "unknown", Timestamp: time.Now()}
			}
		}
	}

	a.addStep(memory.Step{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		Action:    step.Description,
		Tool:      step.Tool,
		Target:    step.Target,
		Result:    result,
		State:     state,
	})

	shouldCheckProgress := false
	if step.Tool == "click" || step.Tool == "type" || step.Tool == "navigate" {
		shouldCheckProgress = true
	} else if step.Tool == "scroll" {
		if a.currentPlan != nil && a.currentPlan.CurrentStep < len(a.currentPlan.Steps)-1 {
			nextStep := a.currentPlan.Steps[a.currentPlan.CurrentStep+1]
			if nextStep.Tool == "observe" || nextStep.Tool == "click" || nextStep.Tool == "type" {
				shouldCheckProgress = true
			}
		}
	}

	if !a.dryRun && state != nil && shouldCheckProgress {
		time.Sleep(200 * time.Millisecond)
		screensDir := a.logger.ScreensDir()
		resultScreenshotPath := fmt.Sprintf("%s/step-%d-result.png", screensDir, a.stepCount)
		if err := a.browser.Screenshot(ctx, resultScreenshotPath); err == nil {
			progressOk, nextAction, err := a.checkProgressWithVision(ctx, resultScreenshotPath, &step, state)
			if err != nil {
				a.logger.Console().Warn("Progress check failed", zap.Error(err))
			} else if !progressOk {
				a.logger.Console().Warn("Progress check indicates plan deviation, updating plan",
					zap.String("expected", step.Description),
					zap.String("next_action", nextAction),
				)
				errorMsg := fmt.Sprintf("Step '%s' completed, but progress check indicates deviation. Next action needed: %s", step.Description, nextAction)
				newPlan, updateErr := a.planner.UpdatePlan(ctx, a.currentPlan, state, errorMsg)
				if updateErr != nil {
					a.logger.Console().Warn("Plan update failed during progress check", zap.Error(updateErr))
				} else {
					a.mu.Lock()
					a.currentPlan = newPlan
					a.mu.Unlock()
					a.logger.Console().Info("Plan updated based on progress check")
				}
			}
		}
	}

	a.mu.Lock()
	a.currentPlan.CurrentStep++
	a.stepCount++
	a.currentStepRetries = 0 // Сбрасываем счетчик при успешном выполнении
	a.mu.Unlock()

	return nil
}

func (a *Agent) executeTool(ctx context.Context, toolName string, args interface{}, target interface{}, screenshotPath string, description string) (interface{}, error) {
	executor, ok := a.toolRegistry.GetExecutor(toolName)
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}

	var argsMap map[string]interface{}
	switch v := args.(type) {
	case nil:
		argsMap = make(map[string]interface{})
	case map[string]interface{}:
		argsMap = v
	default:
		return nil, fmt.Errorf("invalid args type")
	}

	var targetObj *memory.Target
	if target != nil {
		if tMap, ok := target.(map[string]interface{}); ok {
			wrappedArgs := map[string]interface{}{
				"target": tMap,
			}
			var err error
			targetObj, err = tools.ParseTarget(wrappedArgs)
			if err != nil {
				a.logger.Console().Warn("Failed to parse target", zap.Error(err), zap.Any("target", target))
			}
		} else {
			a.logger.Console().Debug("Target is not a map", zap.Any("target", target), zap.String("type", fmt.Sprintf("%T", target)))
		}
	} else {
		a.logger.Console().Debug("Target is nil")
	}

	if a.visionHandler != nil {
		result, handled, err := a.visionHandler.HandleAction(ctx, toolName, argsMap, targetObj, screenshotPath, description)
		if err != nil {
			return nil, fmt.Errorf("vision handler error: %w", err)
		}
		if handled {
			return result, nil
		}
	}

	if toolName == "navigate" {
		if _, exists := argsMap["url"]; !exists {
			url := navigation.ExtractURLFromDescription(description)
			if url != "" {
				argsMap["url"] = url
				a.logger.Console().Info("Extracted URL from description", zap.String("url", url), zap.String("description", description))
			} else {
				url = navigation.ResolveURLFromTask(a.task)
				if url != "" {
					argsMap["url"] = url
					a.logger.Console().Info("Determined URL from task", zap.String("url", url))
				} else {
					if target != nil {
						a.logger.Console().Warn("Planner incorrectly used 'navigate' to find elements, converting to 'observe'",
							zap.String("description", description),
							zap.Any("target", target),
						)
						observeExecutor, ok := a.toolRegistry.GetExecutor("observe")
						if ok {
							observeArgs := map[string]interface{}{
								"focused": false,
								"query":   description,
							}
							return observeExecutor.Execute(ctx, observeArgs)
						}
					}
					return nil, fmt.Errorf("url is required for navigate, but not found in args, description, or task")
				}
			}
		}
		target = nil
	}

	if target != nil && toolName != "navigate" {
		if _, exists := argsMap["target"]; !exists {
			if tMap, ok := target.(map[string]interface{}); ok {
				argsMap["target"] = tMap
			}
		}
	}

	approved, approvalReq := a.security.CheckAction(toolName, argsMap)
	if !approved {
		a.mu.Lock()
		a.approvalReq = approvalReq
		a.mu.Unlock()

		a.logger.Console().Warn("Action requires approval", zap.Any("request", approvalReq))
		return nil, fmt.Errorf("action requires approval")
	}

	return executor.Execute(ctx, argsMap)
}

func (a *Agent) recoverFromError(ctx context.Context, err error) error {
	a.logger.Console().Info("Attempting recovery", zap.Error(err))

	state, observeErr := a.browser.Observe(ctx, false, "")
	if observeErr != nil {
		a.logger.Console().Warn("Observe failed during recovery, using empty state", zap.Error(observeErr))
		state = &memory.PageState{
			URL:        "unknown",
			Title:      "",
			Viewport:   memory.Viewport{Width: 1920, Height: 1080},
			Overlays:   []memory.Overlay{},
			Elements:   []memory.Element{},
			TextDigest: []string{},
			Timestamp:  time.Now(),
		}
	}

	a.mu.Lock()
	oldStep := a.currentPlan.CurrentStep
	a.mu.Unlock()

	newPlan, updateErr := a.planner.UpdatePlan(ctx, a.currentPlan, state, err.Error())
	if updateErr != nil {
		return fmt.Errorf("plan update failed: %w", updateErr)
	}

	a.mu.Lock()
	a.currentPlan = newPlan
	if a.currentPlan.CurrentStep != oldStep {
		a.currentStepRetries = 0
		a.logger.Console().Info("Step changed by Critic, resetting retry counter",
			zap.Int("old_step", oldStep),
			zap.Int("new_step", a.currentPlan.CurrentStep),
		)
	}
	a.mu.Unlock()

	a.logger.Console().Info("Plan updated, continuing")

	return nil
}

func (a *Agent) rollbackToPreviousStep(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.currentPlan == nil {
		return fmt.Errorf("no plan to rollback")
	}

	if a.currentPlan.CurrentStep <= 0 {
		a.logger.Console().Warn("Cannot rollback: already at first step")
		a.currentStepRetries = 0
		return nil
	}

	previousStep := a.currentPlan.CurrentStep - 1
	a.logger.Console().Info("Rolling back to previous step",
		zap.Int("from_step", a.currentPlan.CurrentStep),
		zap.Int("to_step", previousStep),
	)

	a.currentPlan.CurrentStep = previousStep

	a.currentStepRetries = 0

	state, err := a.browser.Observe(ctx, false, "")
	if err == nil {
		state = a.mem.TruncatePageState(state)
		a.mem.SetURL(state.URL)
		a.logger.Console().Info("Rollback successful, current state observed",
			zap.String("url", state.URL),
			zap.String("title", state.Title),
		)
	} else {
		a.logger.Console().Warn("Failed to observe state after rollback", zap.Error(err))
	}

	return nil
}

func (a *Agent) checkWithCritic(ctx context.Context) error {
	state, err := a.browser.Observe(ctx, false, "")
	if err != nil {
		return fmt.Errorf("observe failed: %w", err)
	}

	history := a.mem.GetEphemeral()
	critique, err := a.critic.Critique(ctx, a.task, a.currentPlan, history, state)
	if err != nil {
		return fmt.Errorf("critique failed: %w", err)
	}

	if critique.Blocked {
		a.currentPlan.Blocked = true
		a.currentPlan.BlockReason = critique.BlockReason
		a.logger.Console().Warn("Critic blocked the plan", zap.String("reason", critique.BlockReason))
	}

	if len(critique.Issues) > 0 {
		for _, issue := range critique.Issues {
			a.mem.AddBlocker(issue)
		}
	}

	return nil
}

func (a *Agent) addStep(step memory.Step) {
	a.mem.AddStep(step)
	a.mem.AddCompletedStep(step.Action)
}

func (a *Agent) Pause() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.paused = true
	a.logger.Console().Info("Agent paused")
}

func (a *Agent) Resume() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.paused = false
	a.logger.Console().Info("Agent resumed")
}

func (a *Agent) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopped = true
	a.running = false
	a.logger.Console().Info("Agent stopped")
}

func (a *Agent) Approve() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.approvalReq == nil {
		return fmt.Errorf("no pending approval request")
	}

	a.approvalReq = nil
	a.logger.Console().Info("Action approved")
	return nil
}

func (a *Agent) Deny() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.approvalReq == nil {
		return fmt.Errorf("no pending approval request")
	}

	a.approvalReq = nil
	a.logger.Console().Info("Action denied")
	return nil
}

func (a *Agent) SetMode(dryRun bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dryRun = dryRun
}

func (a *Agent) GetState() AgentState {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.running {
		return StateIdle
	}
	if a.paused {
		return StatePaused
	}
	if a.approvalReq != nil {
		return StateWaiting
	}
	return StateRunning
}

func (a *Agent) GetStatus() map[string]interface{} {
	a.mu.Lock()
	defer a.mu.Unlock()

	status := map[string]interface{}{
		"state":      a.GetState(),
		"task":       a.task,
		"step_count": a.stepCount,
		"dry_run":    a.dryRun,
	}

	if a.currentPlan != nil {
		status["plan_progress"] = fmt.Sprintf("%d/%d", a.currentPlan.CurrentStep, len(a.currentPlan.Steps))
		status["blocked"] = a.currentPlan.Blocked
	}

	if a.approvalReq != nil {
		status["approval_request"] = a.approvalReq
	}

	return status
}

func (a *Agent) isStopped() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stopped
}

func (a *Agent) isPaused() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.paused
}

func (a *Agent) checkProgressWithVision(ctx context.Context, screenshotPath string, completedStep *subagents.Step, currentState *memory.PageState) (bool, string, error) {
	if screenshotPath == "" {
		return true, "", nil
	}

	var prompt strings.Builder
	prompt.WriteString("=== PROGRESS CHECK ===\n\n")
	prompt.WriteString(fmt.Sprintf("TASK: %s\n\n", a.task))
	prompt.WriteString(fmt.Sprintf("COMPLETED STEP: %s\n", completedStep.Description))
	prompt.WriteString(fmt.Sprintf("STEP TOOL: %s\n\n", completedStep.Tool))

	var expectedResult string
	switch completedStep.Tool {
	case "navigate":
		expectedResult = "The page should have navigated to the target URL. Check if the URL changed and the page loaded correctly."
	case "type":
		expectedResult = "Text should have been entered into the input field. Check if the text is visible in the search box or input field."
	case "click":
		expectedResult = "The element should have been clicked. Check if the page changed (navigation occurred, modal opened, button state changed, etc.)."
	case "scroll":
		expectedResult = "The page should have been scrolled. Check if the target element (from the next step) is now visible on the screen, or if new content appeared that helps with the task."
	case "observe":
		expectedResult = "The page state should have been observed. Check if the current page matches what was expected to be observed."
	default:
		expectedResult = "The action should have been completed. Check if the page state matches the expected result."
	}

	prompt.WriteString(fmt.Sprintf("EXPECTED RESULT: %s\n\n", expectedResult))
	prompt.WriteString("CURRENT PAGE CONTEXT:\n")
	prompt.WriteString(fmt.Sprintf("- URL: %s\n", currentState.URL))
	prompt.WriteString(fmt.Sprintf("- Title: %s\n\n", currentState.Title))

	nextStepDesc := ""
	if a.currentPlan != nil && a.currentPlan.CurrentStep < len(a.currentPlan.Steps) {
		nextStep := a.currentPlan.Steps[a.currentPlan.CurrentStep]
		nextStepDesc = nextStep.Description
		prompt.WriteString(fmt.Sprintf("NEXT STEP IN PLAN: %s\n\n", nextStepDesc))
	}

	prompt.WriteString("ANALYSIS TASK:\n")
	prompt.WriteString("1. Examine the screenshot to understand what happened after the completed step\n")
	prompt.WriteString("2. Determine if the expected result was achieved\n")
	prompt.WriteString("3. If the result doesn't match expectations, identify what actually happened\n")
	prompt.WriteString("4. Determine what action is needed next to continue with the task\n")
	prompt.WriteString("5. If the next step in the plan is not applicable, suggest what should be done instead\n\n")

	prompt.WriteString("OUTPUT FORMAT:\n")
	prompt.WriteString("Return your response as a JSON object:\n")
	prompt.WriteString("{\n")
	prompt.WriteString("  \"progress_ok\": <boolean>,  // true if progress matches expectations, false otherwise\n")
	prompt.WriteString("  \"what_happened\": \"<string>\",  // Description of what actually happened on the page\n")
	prompt.WriteString("  \"next_action\": \"<string>\"  // What action is needed next (if progress_ok=false, or if plan needs adjustment)\n")
	prompt.WriteString("}\n")

	checkResult, err := a.analyst.CheckProgress(ctx, screenshotPath, prompt.String())
	if err != nil {
		return true, "", fmt.Errorf("failed to check progress: %w", err)
	}

	type ProgressCheck struct {
		ProgressOk   bool   `json:"progress_ok"`
		WhatHappened string `json:"what_happened"`
		NextAction   string `json:"next_action"`
	}

	a.logger.Console().Info("Progress check completed",
		zap.Bool("progress_ok", checkResult.ProgressOk),
		zap.String("what_happened", checkResult.WhatHappened),
		zap.String("next_action", checkResult.NextAction),
		zap.String("completed_step", completedStep.Description),
	)

	return checkResult.ProgressOk, checkResult.NextAction, nil
}
