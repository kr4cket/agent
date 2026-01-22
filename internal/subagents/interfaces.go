package subagents

import (
	"context"

	"testTask/internal/memory"
)

type PlannerInterface interface {
	CreatePlan(ctx context.Context, task string, currentState *memory.PageState, memoryContext string) (*Plan, error)
	UpdatePlan(ctx context.Context, currentPlan *Plan, currentState *memory.PageState, errorMsg string) (*Plan, error)
}

type AnalystInterface interface {
	AnalyzePage(ctx context.Context, state *memory.PageState, query string, screenshotPath string) (*memory.PageState, error)
	FindClickCoordinates(ctx context.Context, screenshotPath string, target *memory.Target, description string, pageContext ...string) (*ClickCoordinates, error)
	CheckProgress(ctx context.Context, screenshotPath string, prompt string) (*ProgressCheckResult, error)
}

type CriticInterface interface {
	Critique(ctx context.Context, goal string, plan *Plan, history []memory.Step, currentState *memory.PageState) (*Critique, error)
}
