#
# Copyright 2025 Alibaba Group Holding Ltd.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
import json
from datetime import timedelta

import httpx
import pytest

from opensandbox.adapters.command_adapter import CommandsAdapter
from opensandbox.config import ConnectionConfig
from opensandbox.exceptions import SandboxApiException
from opensandbox.models.execd import RunCommandOpts
from opensandbox.models.sandboxes import SandboxEndpoint
from opensandbox.sync.adapters.command_adapter import CommandsAdapterSync


def transport_handler(request):
    if request.url.path == "/execution/instance":
        return httpx.Response(200, json={"instance_id": "scope", "issued_at": 123, "retention_seconds": 86400, "capacity": 4096})
    if request.method == "POST":
        body = json.loads(request.content)
        assert body["operation_id"] == "scope.123.persisted"
        assert body["command"] == "echo hello"
        assert body["cwd"] == "/tmp"
        if request.url.path == "/command/operations":
            assert body["timeout"] == 2000
            assert body["envs"] == {"A": "value"}
    else:
        assert request.headers["X-EXECD-OPERATION-ID"] == "scope.123.persisted"
        if request.url.params["kind"] == "pty":
            return httpx.Response(409, json={"code": "operation_instance_mismatch", "message": "unknown outcome"})
    return httpx.Response(202, json={"id": "original", "kind": "command", "state": "creating", "expires_at": "2026-09-09T00:00:00Z"})


@pytest.mark.asyncio
async def test_async_execution_operations():
    adapter = CommandsAdapter(ConnectionConfig(), SandboxEndpoint(endpoint="localhost:44772"))
    client = httpx.AsyncClient(transport=httpx.MockTransport(transport_handler), base_url="http://localhost:44772")
    adapter._client.set_async_httpx_client(client)
    try:
        instance = await adapter.get_execution_instance()
        assert instance.new_operation_id().startswith("scope.123.")
        opts = RunCommandOpts(working_directory="/tmp", timeout=timedelta(seconds=2), envs={"A": "value"})
        op = await adapter.create_command_operation("scope.123.persisted", "echo hello", opts=opts)
        assert op.id == "original" and op.state == "creating"
        assert (await adapter.get_execution_operation("command", "scope.123.persisted")).id == op.id
        assert (await adapter.create_pty_operation("scope.123.persisted", cwd="/tmp", command="echo hello")).id == op.id
        with pytest.raises(SandboxApiException):
            await adapter.get_execution_operation("pty", "scope.123.persisted")
    finally:
        await client.aclose()
        await adapter._httpx_client.aclose()
        await adapter._sse_client.aclose()


def test_sync_execution_operations():
    adapter = CommandsAdapterSync(ConnectionConfig(), SandboxEndpoint(endpoint="localhost:44772"))
    with httpx.Client(transport=httpx.MockTransport(transport_handler), base_url="http://localhost:44772") as client:
        adapter._client.set_httpx_client(client)
        instance = adapter.get_execution_instance()
        assert instance.new_operation_id().startswith("scope.123.")
        opts = RunCommandOpts(working_directory="/tmp", timeout=timedelta(seconds=2), envs={"A": "value"})
        op = adapter.create_command_operation("scope.123.persisted", "echo hello", opts=opts)
        assert op.id == "original" and op.state == "creating"
        assert adapter.get_execution_operation("command", "scope.123.persisted").id == op.id
        assert adapter.create_pty_operation("scope.123.persisted", cwd="/tmp", command="echo hello").id == op.id
        with pytest.raises(SandboxApiException):
            adapter.get_execution_operation("pty", "scope.123.persisted")
    adapter._httpx_client.close()
    adapter._sse_client.close()
