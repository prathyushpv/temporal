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
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/server/common/log"
	"go.temporal.io/server/common/persistence"
	"go.temporal.io/server/common/persistence/nosql/nosqlplugin/cassandra/gocql"
	"golang.org/x/exp/slices"
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
	templateEnqueueMessageQueryV2 = `INSERT INTO queue_messages (queue_id, queue_partition, message_id, message_payload, message_encoding) VALUES (?, ?, ?, ?, ?) IF NOT EXISTS`
	templateGetMessagesQueryV2    = `SELECT message_id, message_payload, message_encoding FROM queue_messages WHERE queue_id = ? AND queue_partition = ? AND message_id >= ? ORDER BY message_id ASC LIMIT ?`
	templateDeleteMessagesQueryV2 = `DELETE FROM queue_messages WHERE queue_id = ? AND queue_partition = ? AND message_id >= ? AND message_id <= ?`

	templateGetMaxMessageIDQueryV2 = `SELECT MAX(message_id) AS message_id FROM queue_messages WHERE queue_id = ? AND queue_partition = ?`
)

var (
	messageConflictErr             = errors.New("message conflict likely due to concurrent writes")
	deleteMessagesQueryFailedError = errors.New("delete messages query failed")
	deleteMessagesIterFailedClose  = errors.New("delete messages iterator failed to close")
	errGetMaxMessageIDQueryFailed  = errors.New("get max message ID query failed")
	invalidEncodingTypeErr         = errors.New("invalid encoding type for queue message")
)

func (q *queueV2) EnqueueMessage(ctx context.Context, queueID string, blob commonpb.DataBlob) error {
	maxMessageID, err := q.getNextMessageID(ctx, queueID)
	if err != nil {
		return err
	}
	return q.tryInsert(ctx, queueID, blob, maxMessageID)
}

func (q *queueV2) GetMessages(ctx context.Context, queueID string, minMessageID int64, maxCount int) ([]*persistence.QueueV2Message, error) {
	iter := q.session.Query(templateGetMessagesQueryV2, queueID, 0, minMessageID, maxCount).WithContext(ctx).Iter()
	if iter == nil {
		return nil, errGetMaxMessageIDQueryFailed
	}

	var result []*persistence.QueueV2Message
	var messageID int64
	var messagePayload []byte
	var messageEncoding string
	for iter.Scan(&messageID, &messagePayload, &messageEncoding) {
		encoding, ok := enums.EncodingType_value[messageEncoding]
		if !ok {
			return nil, fmt.Errorf("%w: %v", invalidEncodingTypeErr, messageEncoding)
		}
		encodingType := enums.EncodingType(encoding)
		result = append(result, &persistence.QueueV2Message{
			ID: messageID,
			Blob: commonpb.DataBlob{
				EncodingType: encodingType,
				Data:         slices.Clone(messagePayload),
			},
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

func (q *queueV2) DeleteMessages(ctx context.Context, queueID string, minMessageID int64, maxMessageID int) (int, error) {
	iter := q.session.Query(templateDeleteMessagesQueryV2, queueID, 0, minMessageID, maxMessageID).WithContext(ctx).Iter()
	if iter == nil {
		return 0, deleteMessagesQueryFailedError
	}

	nDeleted := 0
	for iter.Scan() {
		nDeleted++
	}
	if err := iter.Close(); err != nil {
		return 0, fmt.Errorf("%w: %v", deleteMessagesIterFailedClose, err)
	}
	return nDeleted, nil
}

func (q *queueV2) tryInsert(ctx context.Context, queueID string, blob commonpb.DataBlob, messageID int64) error {
	applied, err := q.session.Query(
		templateEnqueueMessageQueryV2,
		queueID,
		0,
		messageID,
		blob.Data,
		blob.EncodingType.String(),
	).WithContext(ctx).MapScanCAS(make(map[string]interface{}))
	if err != nil {
		return err
	}
	if !applied {
		return fmt.Errorf("%w: insert with message ID %v was not applied", messageConflictErr, messageID)
	}
	return nil
}

func (q *queueV2) getNextMessageID(ctx context.Context, queueID string) (int64, error) {
	// maxMessageID is a pointer to an int64 because Scan() will set it to nil if there are no rows for the MAX query.
	var maxMessageID *int64
	err := q.session.Query(templateGetMaxMessageIDQueryV2, queueID, 0).WithContext(ctx).Scan(&maxMessageID)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", errGetMaxMessageIDQueryFailed, err)
	}
	if maxMessageID == nil {
		// There are no messages in the queue, so the next message ID is the first message ID, which is 0.
		return 0, nil
	}
	// The next message ID is the max message ID + 1.
	return *maxMessageID + 1, nil
}
