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
	"crypto/sha256"
	"encoding/json"
	"maps"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alibaba/opensandbox/internal/safego"

	"github.com/alibaba/opensandbox/execd/pkg/jupyter/execute"
	"github.com/alibaba/opensandbox/execd/pkg/log"
)

const operationRetention = 24 * time.Hour
const operationCapacity = 4096

// OperationInstance identifies a controller lifetime, not a durable execution store.
type OperationInstance struct {
	InstanceID       string `json:"instance_id"`
	IssuedAt         int64  `json:"issued_at"`
	RetentionSeconds int64  `json:"retention_seconds"`
	Capacity         int    `json:"capacity"`
}

// Operation describes creation only. Use the existing handle APIs for execution status.
type Operation struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	State     string    `json:"state"`
	ExpiresAt time.Time `json:"expires_at"`
}

type OperationError struct {
	Code    string
	Message string
}

func (e *OperationError) Error() string         { return e.Message }
func operationError(code, message string) error { return &OperationError{code, message} }

type operationKey struct {
	principal [32]byte
	kind      string
	identity  string
}
type creationRecord struct {
	operation   Operation
	fingerprint [32]byte
}
type creationRegistry struct {
	sync.Mutex
	once     sync.Once
	instance string
	records  map[operationKey]*creationRecord
	// Internal clock and cap permit deterministic lifecycle tests, not public configuration.
	now      func() time.Time
	capacity int
}

func (c *Controller) initOperations() {
	c.operations.once.Do(func() {
		c.operations.instance = c.newContextID()
		c.operations.records = make(map[operationKey]*creationRecord)
		c.operations.now = time.Now
		c.operations.capacity = operationCapacity
	})
}

func (c *Controller) GetOperationInstance() OperationInstance {
	c.initOperations()
	return OperationInstance{c.operations.instance, c.operations.now().Unix(), int64(operationRetention / time.Second), c.operations.capacity}
}

var operationTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

func (c *Controller) operationExpiry(identity string) (time.Time, error) {
	parts := strings.Split(identity, ".")
	if len(parts) != 3 || len(parts[0]) != 32 || !operationTokenPattern.MatchString(parts[2]) {
		return time.Time{}, operationError("invalid_operation_id", "operation_id must contain instance, server timestamp, and an 8-128 character random token")
	}
	issued, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || issued < 0 || strconv.FormatInt(issued, 10) != parts[1] {
		return time.Time{}, operationError("invalid_operation_id", "invalid operation timestamp")
	}
	if parts[0] != c.operations.instance {
		return time.Time{}, operationError("operation_instance_mismatch", "execd instance changed or identity belongs to another sandbox; previous execution outcome is unknown, do not retry with a new identity")
	}
	if issued > c.operations.now().Unix() {
		return time.Time{}, operationError("invalid_operation_id", "operation timestamp is in the future; use the server-issued timestamp")
	}
	return time.Unix(issued, 0).Add(operationRetention), nil
}

func (c *Controller) claimOperation(principal, kind, identity string, payload any) (*creationRecord, bool, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, false, operationError("invalid_operation_request", "cannot fingerprint operation request")
	}
	fingerprint := sha256.Sum256(encoded)
	c.initOperations()
	c.operations.Lock()
	defer c.operations.Unlock()
	expiry, err := c.operationExpiry(identity)
	if err != nil {
		return nil, false, err
	}
	c.cleanupOperationsLocked()
	key := operationKey{sha256.Sum256([]byte(principal)), kind, identity}
	if entry := c.operations.records[key]; entry != nil {
		if entry.fingerprint != fingerprint {
			return nil, false, operationError("operation_conflict", "operation identity was used with a different request")
		}
		return entry, false, nil
	}
	if !c.operations.now().Before(expiry) {
		return nil, false, operationError("operation_expired", "recovery window expired; previous execution outcome is unknown, creation refused")
	}
	if len(c.operations.records) >= c.operations.capacity {
		return nil, false, operationError("operation_capacity_exceeded", "operation recovery capacity reached; creation refused")
	}
	entry := &creationRecord{Operation{c.newContextID(), kind, "creating", expiry}, fingerprint}
	c.operations.records[key] = entry
	return entry, true, nil
}

func (c *Controller) operationSnapshot(entry *creationRecord) Operation {
	c.operations.Lock()
	defer c.operations.Unlock()
	return entry.operation
}
func (c *Controller) finishCreation(entry *creationRecord, failed bool) {
	c.operations.Lock()
	defer c.operations.Unlock()
	if entry.operation.State != "creating" {
		return
	}
	entry.operation.State = "created"
	if failed {
		entry.operation.State = "failed"
	}
}

// GetOperation is a private lookup, never an inventory of caller identities or inputs.
func (c *Controller) GetOperation(principal, kind, identity string) (Operation, error) {
	c.initOperations()
	c.operations.Lock()
	defer c.operations.Unlock()
	if kind != "command" && kind != "pty" {
		return Operation{}, operationError("invalid_operation_kind", "kind must be command or pty")
	}
	expiry, err := c.operationExpiry(identity)
	if err != nil {
		return Operation{}, err
	}
	c.cleanupOperationsLocked()
	if entry := c.operations.records[operationKey{sha256.Sum256([]byte(principal)), kind, identity}]; entry != nil {
		return entry.operation, nil
	}
	if !c.operations.now().Before(expiry) {
		return Operation{}, operationError("operation_expired", "recovery window expired; previous execution outcome is unknown")
	}
	return Operation{}, operationError("operation_not_found", "operation not found in this authenticated execd instance")
}

func (c *Controller) cleanupOperationsLocked() {
	now := c.operations.now()
	for key, entry := range c.operations.records {
		if entry.operation.State == "creating" || now.Before(entry.operation.ExpiresAt) {
			continue
		}
		if entry.operation.Kind == "command" {
			if kernel := c.commandSnapshot(entry.operation.ID); kernel != nil && kernel.running {
				continue
			}
		} else if !c.expireOperationPTY(entry.operation.ID) {
			continue
		}
		delete(c.operations.records, key)
	}
}
func (c *Controller) cleanupOperations() {
	c.initOperations()
	c.operations.Lock()
	defer c.operations.Unlock()
	c.cleanupOperationsLocked()
}

// CreateCommandOperation claims before scheduling any process creation. It never uses
// request cancellation as execution cancellation and never retains HTTP hooks/writers.
func (c *Controller) CreateCommandOperation(principal, identity string, request *ExecuteCodeRequest) (Operation, error) {
	// Own the inputs used by both fingerprinting and asynchronous startup.
	snapshot := *request
	snapshot.Envs = maps.Clone(request.Envs)
	if request.Uid != nil {
		uid := *request.Uid
		snapshot.Uid = &uid
	}
	if request.Gid != nil {
		gid := *request.Gid
		snapshot.Gid = &gid
	}
	request = &snapshot

	if request.Language != Command && request.Language != BackgroundCommand {
		return Operation{}, operationError("invalid_operation_request", "expected a command request")
	}
	// Explicit typed semantic payload; hooks, context and identity are not command inputs.
	payload := struct {
		Language Language          `json:"language"`
		Code     string            `json:"code"`
		Cwd      string            `json:"cwd"`
		Timeout  time.Duration     `json:"timeout"`
		Envs     map[string]string `json:"envs,omitempty"`
		Uid      *uint32           `json:"uid,omitempty"`
		Gid      *uint32           `json:"gid,omitempty"`
	}{request.Language, request.Code, request.Cwd, request.Timeout, request.Envs, request.Uid, request.Gid}
	entry, owner, err := c.claimOperation(principal, "command", identity, payload)
	if err != nil {
		return Operation{}, err
	}
	if owner {
		req := *request
		req.commandID = entry.operation.ID
		req.Hooks = ExecuteResultHook{
			OnExecuteInit: func(string) {
				if kernel := c.commandSnapshot(req.commandID); kernel != nil {
					c.finishCreation(entry, kernel.pid < 0)
				}
			},
			OnExecuteResult: func(map[string]any, int) {}, OnExecuteStatus: func(string) {},
			OnExecuteStdout: func(string) {}, OnExecuteStderr: func(string) {},
			OnExecuteError: func(*execute.ErrorOutput) {}, OnExecuteComplete: func(time.Duration) {},
		}
		safego.Go(func() {
			// A panic leaves creation unresolved, not permission to start again.
			err := c.Execute(&req)
			kernel := c.commandSnapshot(req.commandID)
			c.finishCreation(entry, err != nil || kernel == nil || kernel.pid < 0)
		})
	}
	return c.operationSnapshot(entry), nil
}

func (c *Controller) CreatePTYOperation(principal, identity, cwd, command string) (Operation, error) {
	entry, owner, err := c.claimOperation(principal, "pty", identity, struct {
		Cwd     string
		Command string
	}{cwd, command})
	if err != nil {
		return Operation{}, err
	}
	if owner {
		_, err := c.createPTYSession(entry.operation.ID, cwd, command, true)
		c.finishCreation(entry, err != nil)
	}
	return c.operationSnapshot(entry), nil
}

func (c *Controller) commandSessionID(request *ExecuteCodeRequest) string {
	if request.commandID != "" {
		return request.commandID
	}
	return c.newContextID()
}

func (request *ExecuteCodeRequest) logCommandReceived() {
	if request.commandID == "" {
		log.Info("received command: %v", log.SanitizeCommand(request.Code))
	}
}
func (request *ExecuteCodeRequest) logCommandError(stage string, err error) {
	if request.commandID == "" {
		log.Error("CommandExecError: error %s commands: %v", stage, err)
	}
}
