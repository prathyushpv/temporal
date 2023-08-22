package queues

import (
	"context"

	"go.temporal.io/server/service/history/tasks"
)

type (
	DLQ interface {
		// AddTask adds a task to the DLQ
		AddTask(ctx context.Context, task tasks.Task) error
	}
	noopDLQ struct{}
)

// NewNoopDLQ returns a DLQ that does nothing
func NewNoopDLQ() DLQ {
	return noopDLQ{}
}

func (n noopDLQ) AddTask(context.Context, tasks.Task) error {
	return nil
}
