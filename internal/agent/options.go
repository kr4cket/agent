package agent

type AgentOptions struct {
	MaxSteps       int
	CriticInterval int
	DryRun         bool

	ViewportWidth  int
	ViewportHeight int
}

func NewAgentOptions(maxSteps, criticInterval int, dryRun bool, viewportWidth, viewportHeight int) AgentOptions {
	return AgentOptions{
		MaxSteps:       maxSteps,
		CriticInterval: criticInterval,
		DryRun:         dryRun,
		ViewportWidth:  viewportWidth,
		ViewportHeight: viewportHeight,
	}
}
