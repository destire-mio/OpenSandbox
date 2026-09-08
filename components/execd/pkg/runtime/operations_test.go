// Copyright 2025 Alibaba Group Holding Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package runtime

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testOperationID(c *Controller, suffix string) string {
	i := c.GetOperationInstance()
	return fmt.Sprintf("%s.%d.%s", i.InstanceID, i.IssuedAt, suffix)
}

func requireOperationError(t *testing.T, err error, code string) {
	t.Helper()
	var e *OperationError
	require.ErrorAs(t, err, &e)
	require.Equal(t, code, e.Code)
}

func TestOperationClaimAndRetention(t *testing.T) {
	c := NewController("", "")
	c.initOperations()
	now := time.Unix(1700000000, 0)
	c.operations.now = func() time.Time { return now }
	c.operations.capacity = 2
	key := testOperationID(c, "first-key")
	entry, owner, err := c.claimOperation("owner", "command", key, "same")
	require.NoError(t, err)
	require.True(t, owner)
	// Claim is paused before launch. Competitors must observe the same creating handle.
	gate := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			next, won, err := c.claimOperation("owner", "command", key, "same")
			if err != nil || won || next != entry {
				t.Errorf("duplicate acquired creation: %v %v", won, err)
			}
		}()
	}
	close(gate)
	wg.Wait()
	_, _, err = c.claimOperation("owner", "command", key, "different")
	requireOperationError(t, err, "operation_conflict")
	_, err = c.GetOperation("other", "command", key)
	requireOperationError(t, err, "operation_not_found")
	second, owner, err := c.claimOperation("other", "command", key, "same")
	require.NoError(t, err)
	require.True(t, owner)
	require.NotEqual(t, entry.operation.ID, second.operation.ID)
	_, _, err = c.claimOperation("owner", "command", testOperationID(c, "third-key"), "same")
	requireOperationError(t, err, "operation_capacity_exceeded")
	now = now.Add(operationRetention + time.Second)
	// In-progress entries survive TTL AND capacity pressure; an old token is never reissued.
	got, err := c.GetOperation("owner", "command", key)
	require.NoError(t, err)
	require.Equal(t, "creating", got.State)
	c.finishCreation(entry, true)
	_, err = c.GetOperation("owner", "command", key)
	requireOperationError(t, err, "operation_expired")
	_, _, err = c.claimOperation("owner", "command", key, "same")
	requireOperationError(t, err, "operation_expired")
	require.Len(t, c.operations.records, 1)
}

func TestOperationActiveCleanupAndRestart(t *testing.T) {
	c := NewController("", "")
	c.initOperations()
	now := time.Unix(1700000000, 0)
	c.operations.now = func() time.Time { return now }
	key := testOperationID(c, "active-key")
	entry, _, err := c.claimOperation("owner", "command", key, "payload")
	require.NoError(t, err)
	c.storeCommandKernel(entry.operation.ID, &commandKernel{running: true})
	c.finishCreation(entry, false)
	now = now.Add(operationRetention + time.Second)
	op, err := c.GetOperation("owner", "command", key)
	require.NoError(t, err)
	require.Equal(t, entry.operation.ID, op.ID)
	c.markCommandFinished(op.ID, 0, "")
	_, err = c.GetOperation("owner", "command", key)
	requireOperationError(t, err, "operation_expired")
	replacement := NewController("", "")
	_, err = replacement.GetOperation("owner", "command", key)
	requireOperationError(t, err, "operation_instance_mismatch")
	_, _, err = replacement.claimOperation("owner", "command", key, "payload")
	requireOperationError(t, err, "operation_instance_mismatch")
}

func TestOperationFailureAndFingerprint(t *testing.T) {
	c := NewController("", "")
	key := testOperationID(c, "fingerprint")
	req := &ExecuteCodeRequest{Language: Command, Code: "echo secret", Cwd: "/definitely-not-an-existing-operation-directory", Envs: map[string]string{"A": "secret", "B": "value"}}
	first, err := c.CreateCommandOperation("owner", key, req)
	require.NoError(t, err)
	require.Eventually(t, func() bool { op, _ := c.GetOperation("owner", "command", key); return op.State == "failed" }, 5*time.Second, 10*time.Millisecond)
	retry, err := c.CreateCommandOperation("owner", key, req)
	require.NoError(t, err)
	require.Equal(t, first.ID, retry.ID)
	require.Equal(t, "failed", retry.State)
	copy := *req
	copy.Envs = map[string]string{"B": "value", "A": "secret"}
	retry, err = c.CreateCommandOperation("owner", key, &copy)
	require.NoError(t, err)
	require.Equal(t, first.ID, retry.ID)
	for _, mutate := range []func(*ExecuteCodeRequest){
		func(r *ExecuteCodeRequest) { r.Code = "other" }, func(r *ExecuteCodeRequest) { r.Cwd = "/tmp" },
		func(r *ExecuteCodeRequest) { r.Language = BackgroundCommand }, func(r *ExecuteCodeRequest) { r.Timeout = time.Second },
		func(r *ExecuteCodeRequest) { v := uint32(1); r.Uid = &v }, func(r *ExecuteCodeRequest) { v := uint32(1); r.Gid = &v },
		func(r *ExecuteCodeRequest) { r.Envs = map[string]string{"A": "different"} },
	} {
		copy := *req
		mutate(&copy)
		_, err = c.CreateCommandOperation("owner", key, &copy)
		requireOperationError(t, err, "operation_conflict")
		require.NotContains(t, err.Error(), "secret")
	}
}
