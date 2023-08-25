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

package cassandra

import (
	"context"
	"errors"
	"fmt"

	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/server/common/log"
	"go.temporal.io/server/common/persistence"
	"go.temporal.io/server/common/persistence/nosql/nosqlplugin/cassandra/gocql"
)

type (
	// queueV2 contains the SQL queries and serialization/deserialization functions to interact with the queues and
	// queue_messages tables that implement the QueueV2 interface. The schema is located at:
	//	schema/cassandra/temporal/versioned/v1.9/queue_v2.cql
	queueV2 struct {
		session gocql.Session
		logger  log.Logger
	}
)

const (
	templateEnqueueMessageQueryV2 = `INSERT INTO queue_messages (queue_id, queue_partition, message_id, message_payload, message_encoding, version) VALUES (?, ?, ?, ?, ?, ?) IF NOT EXISTS`
	templateGetMessagesQueryV2    = `SELECT message_id, message_payload, message_encoding FROM queue_messages WHERE queue_id = ? AND queue_partition = ? AND message_id >= ? ORDER BY message_id ASC LIMIT ?`

	templateGetMaxMessageIDQueryV2 = `SELECT MAX(message_id) FROM queue_messages WHERE queue_type = ? AND queue_name = ? AND queue_partition = ?`
)

var (
	messageConflictError = errors.New("message conflict likely due to concurrent writes")
)

func (q *queueV2) EnqueueMessage(ctx context.Context, queueType persistence.QueueV2Type, queueName string, blob *commonpb.DataBlob) error {
	maxMessageID, err := q.getMaxMessageID(ctx, queueType, queueName)
	if err != nil {
		return err
	}
	return q.tryInsert(ctx, queueType, queueName, blob, err, maxMessageID)
}

func (q *queueV2) GetMessages(ctx context.Context, queueType persistence.QueueV2Type, queueID string, lastMessageID int64, maxCount int) ([]*persistence.QueueV2Message, error) {
	iter := q.session.Query(templateGetMessagesQueryV2, queueType, queueID, lastMessageID, maxCount).WithContext(ctx).Iter()
	if iter == nil {
		return nil, fmt.Errorf("unable to get iterator for query")
	}

	var result []*persistence.QueueV2Message
	var messageID int64
	var messagePayload []byte
	var messageEncoding string
	for iter.Scan(&messageID, &messagePayload, &messageEncoding) {
		result = append(result, &persistence.QueueV2Message{
			ID:       messageID,
			Data:     messagePayload,
			Encoding: messageEncoding,
		})
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return result, nil
}

func newQueueV2(session gocql.Session, logger log.Logger) persistence.QueueV2 {
	return &queueV2{
		session: session,
		logger:  logger,
	}
}

func (q *queueV2) tryInsert(ctx context.Context, queueType persistence.QueueV2Type, queueName string, blob *commonpb.DataBlob, err error, maxMessageID int64) error {
	applied, err := q.session.Query(
		templateEnqueueMessageQueryV2,
		queueType,
		queueName,
		0,
		maxMessageID,
		blob.Data,
		blob.EncodingType.String(),
		0,
	).WithContext(ctx).MapScanCAS(make(map[string]interface{}))
	if err != nil {
		return err
	}
	if !applied {
		return fmt.Errorf("%w: %v", messageConflictError, maxMessageID)
	}
	return nil
}

func (q *queueV2) getMaxMessageID(ctx context.Context, queueType persistence.QueueV2Type, queueName string) (int64, error) {
	result := make(map[string]interface{})
	err := q.session.Query(templateGetMaxMessageIDQueryV2, queueType, queueName, 0).WithContext(ctx).MapScan(result)
	if err != nil {
		if !gocql.IsNotFoundError(err) {
			return 0, err
		}
		return 0, nil
	}
	return result["message_id"].(int64) + 1, nil
}
