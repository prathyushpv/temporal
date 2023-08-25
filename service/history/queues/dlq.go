// The MIT License
//
// Copyright (c) 2020 Temporal Technologies Inc.  All rights reserved.
//
// Copyright (c) 2020 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package queues

import (
	"context"
	"errors"
	"fmt"

	"go.temporal.io/server/common/persistence"
	"go.temporal.io/server/common/persistence/serialization"
	"go.temporal.io/server/service/history/tasks"
)

type (
	DLQ interface {
		// AddTask adds a task to the DLQ
		AddTask(ctx context.Context, task tasks.Task) error
		// ReadTasks reads tasks from the DLQ
		ReadTasks(ctx context.Context, category tasks.Category, dlqMessageID int64, maxCount int) ([]*DLQTask, error)
	}
	DLQTask struct {
		MessageID int64
		Task      tasks.Task
	}
	noopDLQ struct{}
)

var (
	serializeTaskErr    = errors.New("failed to serialize task when adding to DLQ")
	getQueueMessagesErr = errors.New("failed to get messages from DLQ")
)

// NewNoopDLQ returns a DLQ that does nothing
func NewNoopDLQ() DLQ {
	return noopDLQ{}
}

func (n noopDLQ) AddTask(context.Context, tasks.Task) error {
	return nil
}

func (n noopDLQ) ReadTasks(ctx context.Context, category tasks.Category, dlqMessageID int64, maxCount int) ([]*DLQTask, error) {
	return nil, nil
}

type dlq struct {
	queue      persistence.QueueV2
	serializer *serialization.TaskSerializer
}

// NewDLQ returns a DLQ that uses the QueueV2 interface
func NewDLQ(queue persistence.QueueV2) DLQ {
	return &dlq{
		queue:      queue,
		serializer: serialization.NewTaskSerializer(),
	}
}

func (q *dlq) AddTask(ctx context.Context, task tasks.Task) error {
	queueID := q.getQueueIDForTaskCategory(task.GetCategory())
	blob, err := q.serializer.SerializeTask(task)
	if err != nil {
		return fmt.Errorf("%w: %v", serializeTaskErr, err)
	}
	return q.queue.EnqueueMessage(ctx, queueID, blob)
}

func (q *dlq) getQueueIDForTaskCategory(category tasks.Category) string {
	return fmt.Sprintf("history-dlq-%d", category.ID())
}

func (q *dlq) ReadTasks(ctx context.Context, category tasks.Category, dlqMessageID int64, maxCount int) ([]*DLQTask, error) {
	messages, err := q.queue.GetMessages(ctx, q.getQueueIDForTaskCategory(category), dlqMessageID, maxCount)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", getQueueMessagesErr, err)
	}
	dlqTasks := make([]*DLQTask, 0, len(messages))
	for _, message := range messages {
		task, err := q.serializer.DeserializeTask(category, message.Blob)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", getQueueMessagesErr, err)
		}
		dlqTasks = append(dlqTasks, &DLQTask{
			MessageID: message.ID,
			Task:      task,
		})
	}
	return dlqTasks, nil
}
