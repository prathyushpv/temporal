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

package persistencetests

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/api/enums/v1"
)

func RunQueueV2TestSuite(t *testing.T, testBase *TestBase) {
	queue := testBase.QueueV2
	testQueue := "test-queue-" + t.Name()
	t.Cleanup(func() {
		_, err := queue.DeleteMessages(context.Background(), testQueue, 0, 1000)
		assert.NoError(t, err)
	})

	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	t.Cleanup(cancel)

	messages, err := queue.GetMessages(ctx, testQueue, 0, 1)
	require.NoError(t, err)
	assert.Equal(t, 0, len(messages))

	err = queue.EnqueueMessage(ctx, testQueue, &commonpb.DataBlob{
		EncodingType: enums.ENCODING_TYPE_PROTO3, // not actually proto3, but doesn't matter for this test
		Data:         []byte("message1"),
	})
	require.NoError(t, err)

	messages, err = queue.GetMessages(ctx, testQueue, 0, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, len(messages))
	assert.Equal(t, int64(1), messages[0].ID)
	assert.Equal(t, []byte("message1"), messages[0].Data)
	assert.Equal(t, enums.ENCODING_TYPE_PROTO3.String(), messages[0].Encoding)

	err = queue.EnqueueMessage(ctx, testQueue, &commonpb.DataBlob{
		EncodingType: enums.ENCODING_TYPE_PROTO3, // not actually proto3, but doesn't matter for this test
		Data:         []byte("message2"),
	})
	require.NoError(t, err)

	messages, err = queue.GetMessages(ctx, testQueue, 0, 2)
	require.NoError(t, err)
	assert.Equal(t, 2, len(messages))
	assert.Equal(t, int64(1), messages[0].ID)
	assert.Equal(t, []byte("message1"), messages[0].Data)
	assert.Equal(t, enums.ENCODING_TYPE_PROTO3.String(), messages[0].Encoding)
	assert.Equal(t, int64(2), messages[1].ID)
	assert.Equal(t, []byte("message2"), messages[1].Data)
	assert.Equal(t, enums.ENCODING_TYPE_PROTO3.String(), messages[1].Encoding)

	messages, err = queue.GetMessages(ctx, testQueue, 2, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, len(messages))
	assert.Equal(t, int64(2), messages[0].ID)
	assert.Equal(t, []byte("message2"), messages[0].Data)
	assert.Equal(t, enums.ENCODING_TYPE_PROTO3.String(), messages[0].Encoding)
}
