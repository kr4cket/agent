package agent

import (
	"context"
)

type AgentController interface {
	Start(ctx context.Context, task string) error
	Stop()
	Pause()
	Resume()
	Approve() error
	Deny() error
	SetMode(dryRun bool)
	GetStatus() map[string]interface{}
}

var _ AgentController = (*Agent)(nil)
