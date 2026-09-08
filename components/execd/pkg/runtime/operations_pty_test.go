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

//go:build !windows

package runtime

import (
	"github.com/stretchr/testify/require"
	"os"
	"testing"
	"time"
)

func TestOperationPTYFailedLaunchNeverReattempts(t *testing.T) {
	for _, pipe := range []bool{false, true} {
		name := "pty"
		if pipe {
			name = "pipe"
		}
		t.Run(name, func(t *testing.T) {
			c := NewController("", "")
			cwd := t.TempDir()
			op, err := c.CreatePTYOperation("owner", testOperationID(c, "failed-pty-start"), cwd, "true")
			require.NoError(t, err)
			s := c.GetPTYSession(op.ID)
			defer c.DeletePTYSession(op.ID)
			require.True(t, s.LockWS())
			defer s.UnlockWS()
			require.NoError(t, os.Remove(cwd))
			start := s.StartPTY
			if pipe {
				start = s.StartPipe
			}
			require.Error(t, start())
			require.NoError(t, os.Mkdir(cwd, 0700))
			require.ErrorContains(t, start(), "already attempted")
			require.False(t, s.IsRunning())
		})
	}
}

func TestOperationPTYRetention(t *testing.T) {
	c := NewController("", "")
	c.initOperations()
	now := time.Unix(1700000000, 0)
	c.operations.now = func() time.Time { return now }
	dormantKey := testOperationID(c, "dormant-pty")
	dormant, err := c.CreatePTYOperation("owner", dormantKey, "", "")
	require.NoError(t, err)
	activeKey := testOperationID(c, "active-pty")
	active, err := c.CreatePTYOperation("owner", activeKey, "", "read value")
	require.NoError(t, err)
	session := c.GetPTYSession(active.ID)
	require.NotNil(t, session)
	require.True(t, session.LockWS())
	defer session.UnlockWS()
	require.NoError(t, session.StartPipe())
	defer c.DeletePTYSession(active.ID)
	now = now.Add(operationRetention + time.Second)
	_, err = c.GetOperation("owner", "pty", dormantKey)
	requireOperationError(t, err, "operation_expired")
	require.Nil(t, c.GetPTYSession(dormant.ID))
	op, err := c.GetOperation("owner", "pty", activeKey)
	require.NoError(t, err)
	require.Equal(t, active.ID, op.ID)
	require.True(t, session.IsRunning())
	_, err = session.WriteStdin([]byte("done\n"))
	require.NoError(t, err)
	select {
	case <-session.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("PTY process did not exit")
	}
	// Neither the other launch mode nor the same mode can create a new process.
	require.Error(t, session.StartPipe())
	require.Error(t, session.StartPTY())
	_, err = c.GetOperation("owner", "pty", activeKey)
	requireOperationError(t, err, "operation_expired")
	require.Nil(t, c.GetPTYSession(active.ID))
}
