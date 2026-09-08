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

import assert from "node:assert/strict";
import test from "node:test";
import { CommandsAdapter, createExecdClient } from "../dist/internal.js";
import { newOperationId, SandboxApiException } from "../dist/index.js";

test("operation requests preserve identity, map fields, and do not claim execution success", async () => {
  const requests = [];
  const fetchImpl = async (request) => {
    requests.push(request);
    const url = new URL(request.url);
    if (url.pathname === "/execution/instance") return Response.json({ instance_id: "scope", issued_at: 123, retention_seconds: 86400, capacity: 4096 });
    if (request.method === "POST") {
      const body = await request.json();
      assert.equal(body.operation_id, "scope.123.persisted");
      assert.equal(body.command, "echo hello");
      assert.equal(body.cwd, "/tmp");
      if (url.pathname === "/command/operations") { assert.equal(body.timeout, 2000); assert.deepEqual(body.envs, { A: "value" }); }
    } else {
      assert.equal(request.headers.get("X-EXECD-OPERATION-ID"), "scope.123.persisted");
      if (url.searchParams.get("kind") === "pty") return Response.json({ code: "operation_instance_mismatch", message: "unknown outcome" }, { status: 409 });
    }
    return Response.json({ id: "original", kind: "command", state: "creating", expires_at: "2026-09-09T00:00:00Z" }, { status: 202 });
  };
  const client = createExecdClient({ baseUrl: "http://localhost:44772", fetch: fetchImpl });
  const adapter = new CommandsAdapter(client, { baseUrl: "http://localhost:44772", fetch: fetchImpl });
  const instance = await adapter.getExecutionInstance();
  assert.match(newOperationId(instance), /^scope\.123\.[a-f0-9]{32}$/);
  const op = await adapter.createCommandOperation("scope.123.persisted", "echo hello", { workingDirectory: "/tmp", timeoutSeconds: 2, envs: { A: "value" } });
  assert.equal(op.state, "creating");
  assert.equal((await adapter.getExecutionOperation("command", "scope.123.persisted")).id, op.id);
  assert.equal((await adapter.createPTYOperation("scope.123.persisted", { cwd: "/tmp", command: "echo hello" })).id, op.id);
  await assert.rejects(adapter.getExecutionOperation("pty", "scope.123.persisted"), SandboxApiException);
  assert.equal(requests.length, 5);
});
