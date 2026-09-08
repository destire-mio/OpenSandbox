#
# Copyright 2026 Alibaba Group Holding Ltd.
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

from __future__ import annotations

from collections.abc import Mapping
from typing import TYPE_CHECKING, Any, TypeVar

from attrs import define as _attrs_define
from attrs import field as _attrs_field

from ..types import UNSET, Unset

if TYPE_CHECKING:
    from ..models.run_command_request_envs import RunCommandRequestEnvs


T = TypeVar("T", bound="CreateCommandOperationRequest")


@_attrs_define
class CreateCommandOperationRequest:
    """
    Attributes:
        command (str): Shell command to execute Example: ls -la /workspace.
        operation_id (str): Optional caller identity: <instance_id>.<issued_at Unix seconds>.<8-128 character random
            token>. Obtain instance_id and issued_at from GET /execution/instance, generate token and persist the entire ID
            before sending. Scope is the authenticated execd controller and kind; clients sharing its configured token share
            one principal. Recovery lasts 24 hours from issued_at, extended while creating or active. Capacity is 4096
            records/controller; new claims fail closed at capacity. Expired terminal/dormant records are removed; old
            expired IDs return 410 instead of recreating. Controller restart or another sandbox returns 409
            operation_instance_mismatch: outcome unknown. Memory and OS process creation are not a transaction. No cross-
            execd-restart reconciliation, exactly-once completion, or business-side-effect guarantee. Never regenerate any
            identity component during retry. Use an opaque random token, never a secret or business payload.
        cwd (str | Unset): Working directory for command execution Example: /workspace.
        background (bool | Unset): Whether to run command in detached mode Default: False.
        timeout (int | Unset): Maximum allowed execution time in milliseconds before the command is forcefully
            terminated by the server. If omitted, the server will not enforce any timeout. Example: 60000.
        uid (int | Unset): Unix user ID used to run the command. If `gid` is provided, `uid` is required.
             Example: 1000.
        gid (int | Unset): Unix group ID used to run the command. Requires `uid` to be provided.
             Example: 1000.
        envs (RunCommandRequestEnvs | Unset): Environment variables injected into the command process. Example: {'PATH':
            '/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin', 'PYTHONUNBUFFERED': '1'}.
    """

    command: str
    operation_id: str
    cwd: str | Unset = UNSET
    background: bool | Unset = False
    timeout: int | Unset = UNSET
    uid: int | Unset = UNSET
    gid: int | Unset = UNSET
    envs: RunCommandRequestEnvs | Unset = UNSET
    additional_properties: dict[str, Any] = _attrs_field(init=False, factory=dict)

    def to_dict(self) -> dict[str, Any]:
        command = self.command

        operation_id = self.operation_id

        cwd = self.cwd

        background = self.background

        timeout = self.timeout

        uid = self.uid

        gid = self.gid

        envs: dict[str, Any] | Unset = UNSET
        if not isinstance(self.envs, Unset):
            envs = self.envs.to_dict()

        field_dict: dict[str, Any] = {}
        field_dict.update(self.additional_properties)
        field_dict.update(
            {
                "command": command,
                "operation_id": operation_id,
            }
        )
        if cwd is not UNSET:
            field_dict["cwd"] = cwd
        if background is not UNSET:
            field_dict["background"] = background
        if timeout is not UNSET:
            field_dict["timeout"] = timeout
        if uid is not UNSET:
            field_dict["uid"] = uid
        if gid is not UNSET:
            field_dict["gid"] = gid
        if envs is not UNSET:
            field_dict["envs"] = envs

        return field_dict

    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.run_command_request_envs import RunCommandRequestEnvs

        d = dict(src_dict)
        command = d.pop("command")

        operation_id = d.pop("operation_id")

        cwd = d.pop("cwd", UNSET)

        background = d.pop("background", UNSET)

        timeout = d.pop("timeout", UNSET)

        uid = d.pop("uid", UNSET)

        gid = d.pop("gid", UNSET)

        _envs = d.pop("envs", UNSET)
        envs: RunCommandRequestEnvs | Unset
        if isinstance(_envs, Unset):
            envs = UNSET
        else:
            envs = RunCommandRequestEnvs.from_dict(_envs)

        create_command_operation_request = cls(
            command=command,
            operation_id=operation_id,
            cwd=cwd,
            background=background,
            timeout=timeout,
            uid=uid,
            gid=gid,
            envs=envs,
        )

        create_command_operation_request.additional_properties = d
        return create_command_operation_request

    @property
    def additional_keys(self) -> list[str]:
        return list(self.additional_properties.keys())

    def __getitem__(self, key: str) -> Any:
        return self.additional_properties[key]

    def __setitem__(self, key: str, value: Any) -> None:
        self.additional_properties[key] = value

    def __delitem__(self, key: str) -> None:
        del self.additional_properties[key]

    def __contains__(self, key: str) -> bool:
        return key in self.additional_properties
