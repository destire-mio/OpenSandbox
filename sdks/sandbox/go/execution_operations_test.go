// Copyright 2026 Alibaba Group Holding Ltd.
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

package opensandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExecutionOperationsMapping(t *testing.T) {
	key := "instance.timestamp.caller"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, "token", r.Header.Get("X-EXECD-ACCESS-TOKEN"))
		switch r.URL.Path {
		case "/execution/instance":
			fmt.Fprint(w, `{"instance_id":"scope","issued_at":123,"retention_seconds":86400,"capacity":4096}`)
			return
		case "/execution/operation":
			require.Equal(t, key, r.Header.Get("X-EXECD-OPERATION-ID"))
			require.Equal(t, "command", r.URL.Query().Get("kind"))
		case "/command/operations", "/pty/operations":
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Equal(t, key, body["operation_id"])
			require.Equal(t, "echo hello", body["command"])
			require.Equal(t, "/tmp", body["cwd"])
			w.WriteHeader(202)
		}
		fmt.Fprint(w, `{"id":"original","kind":"command","state":"creating","expires_at":"2026-09-09T00:00:00Z"}`)
	}))
	defer server.Close()
	client := NewExecdClient(server.URL, "token")
	ctx := context.Background()
	instance, err := client.GetExecutionInstance(ctx)
	require.NoError(t, err)
	identity, err := instance.NewOperationID()
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(identity, "scope.123."))
	op, err := client.CreateCommandOperation(ctx, key, RunCommandRequest{Command: "echo hello", Cwd: "/tmp"})
	require.NoError(t, err)
	require.Equal(t, "creating", op.State)
	op, err = client.GetExecutionOperation(ctx, "command", key)
	require.NoError(t, err)
	require.Equal(t, "original", op.ID)
	_, err = client.CreatePTYOperation(ctx, key, "/tmp", "echo hello")
	require.NoError(t, err)
	require.Equal(t, 4, calls)
	_, err = client.CreateCommandOperation(ctx, "", RunCommandRequest{})
	require.Error(t, err)
	require.Equal(t, 4, calls)
}
