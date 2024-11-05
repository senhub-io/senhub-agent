package agent

import "context"

type Agent interface {
	ListenAndServe() error
	Shutdown(context.Context) error
}

type agent struct{}

// Create new agent from context
func NewAgent() Agent {
	return agent{}
}

func (a agent) ListenAndServe() error {
	return nil
}
func (a agent) Shutdown(ctx context.Context) error {
	return nil
}
