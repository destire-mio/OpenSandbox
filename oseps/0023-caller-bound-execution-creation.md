---
title: Caller-bound execution creation
authors:
  - "@destire-mio"
creation-date: 2026-09-08
last-updated: 2026-09-09
status: draft
---

# OSEP-0023: Caller-bound execution creation

## Summary

Opt-in caller-bound operation identities recover command and PTY create handles after a lost response or caller crash, without creating a second process. This proposal and experimental implementation address #1547 and are pending maintainer design review. Proposal number 0023 is provisional and must be reassigned if upstream allocates it first.

## Motivation

Command IDs are generated per request and published around process startup. If a caller loses the first response, ordinary retry assigns another ID and starts another process. A baseline real-HTTP test produced two handles and two PID markers for both foreground and background commands. PTY POST creates a dormant session; its process starts on the first WebSocket connection. It needs a recoverable session identity and a guard against launching again after terminal reconnect.

### Goals

- Recover the original command or PTY handle using an identity persisted before sending.
- Atomically claim identity and fingerprint before creation; concurrent duplicates share one handle.
- Bound metadata, distinguish authenticated runtime scope, reject stale-instance/expired identities without recreating.
- Keep spec and five SDKs aligned, with actual HTTP/process and PTY/replay/takeover tests.

### Non-Goals

Exactly-once script completion or arbitrary business effects; durable cross-execd-restart reconciliation; workflow databases, command history, general audit services, SSE resumption, or automatic retries for arbitrary POST requests.

## Requirements

Existing clients retain their current command SSE and PTY JSON behavior; their paths, request schemas and generated command return types are preserved. Command and PTY creation belong to this proposal; implementing only command recovery does not close #1547. Existing command status/log and PTY lifecycle objects remain authoritative for execution state.

## Proposal

Add `/command/operations` and `/pty/operations` create routes with required `operation_id`, `GET /execution/instance` to obtain server scope/time, and a private `GET /execution/operation?kind=...` lookup with `X-EXECD-OPERATION-ID` header. These new routes return JSON creation acknowledgement; the existing command route remains SSE. High-level SDK methods are distinct from ordinary streaming `run`, preventing accidental interpretation as a completed execution.

The full protocol, errors, field normalization, lifecycle cases and SDK method mapping are in [the recovery guide](../docs/guides/execution-creation-recovery.md) and [the source spec](../specs/execd-api.yaml).

### Notes/Constraints/Caveats

- An identity has form `<controller ID>.<server issued_at>.<caller random token>`. The caller persists all components and the immutable request before sending. It must never regenerate scope/time on recovery.
- Scope is controller/sandbox + authenticated principal + resource kind. The current single-token execd authentication model does not separate users who share that token. Anonymous mode shares one boundary.
- Creation state (`creating`, `created`, `failed`) is separate from execution state. `created` PTY means dormant session created; it does not claim shell startup.
- The guarantee covers a caller restart within one execd lifetime/window. An execd replacement rejects the old instance with an explicit unknown-outcome error. Registration and OS spawn are not transactional.

### Risks and Mitigations

A request may disappear after claim but before startup. The runtime owns creation independently of the socket, keeps the reservation, and never hands ownership to another retry. If execution panics while creating, the record remains unresolved rather than permitting another launch. Capacity exhaustion refuses a new claim rather than evicting active or unexpired entries.

A dormant PTY can race expiry and first connection. Its session lock serializes expiry with `StartPTY`/`StartPipe`. A caller-bound PTY permits one launch attempt; a terminal reconnect uses existing replay/terminal behavior. Unkeyed behavior is preserved.

Sensitive command/env data is used only to compute an internal SHA-256 request fingerprint. New errors, creation lookup and keyed command status do not expose raw input. No identity enumeration route is added. Authentication runs before create/lookup, and recovery responses are not cacheable.

## Design Details

The existing `runtime.Controller` owns a bounded creation registry. Its entries contain only reserved handle, kind, fingerprint, creation disposition and expiry. Command state stays in `commandKernel`; PTY state stays in `ptySession`. The command launch path uses a reserved ID, registers the existing kernel, and publishes the creation result. Lookup does not maintain a parallel copy of exit status or output.

Under a registry mutex, a claim checks instance, retention, capacity and fingerprint. Only an absent valid identity can obtain creator ownership. Matching duplicates return the original handle even while startup is pending; mismatches return HTTP 409. Validation that depends on filesystem state belongs to the creator, not recovery. Static validation includes field types, positive timeout and UID/GID constraints.

Retention is 24 hours from the encoded server timestamp, extended while creating/active; cap is 4096 total records per controller. Expired inactive records are collected on admission/lookup and by the existing hourly janitor. Because the timestamp remains in the supplied identity, an evicted expired token can be rejected without permanent tombstones. Expiry can remove dormant or terminal keyed PTY resources, never active sessions. Existing known-ID command retention stays independent. Explicit deletion can invalidate a handle while its creation record remains; retry still cannot recreate it.

Fingerprint inputs are command text, cwd, background, timeout, uid, gid and environment map; PTY uses command/cwd. Canonical typed JSON sorts map keys and normalizes absent/empty env maps. Do not trim command strings or infer shell equivalence. Unknown keyed request fields are rejected. Daemon environment is resolved at the winning launch; PTY/pipe mode follows the first WebSocket launch and is not a POST parameter.

## Test Plan

- Baseline: send a real create, discard its response, resend same logical operation, inspect handles and PID marker count.
- Fixed HTTP: fault proxy waits for startup marker, drops downstream response; retry/query recovers handle and count is one, foreground/background.
- Controlled simultaneous identical claims; different identities; mismatches across every fingerprint field; auth/kind/controller separation and no secret leakage.
- Startup failures, terminal nonzero exit, cancellation, missing cwd after completion, expired/future/old-instance keys, cap and active cleanup.
- PTY response loss, dormant state, first attach, replay/takeover, terminal reconnect, expired dormant/terminal cleanup and live protection.
- Linux and macOS runtime/Web race tests, existing package checks, standalone daemon smoke, five SDK mapping/error tests and generators, docs build and license/format checks. Report unrun checks in the PR description, not as passing CI.

## Drawbacks

The identity is structured rather than entirely opaque, and creation requires one initial scope/time lookup. Fixed retention/cap impose an admission ceiling. Keyed foreground creation returns acknowledgement only, so clients needing output should select background logs or use future SSE-resumption work. A stuck creating entry consumes capacity until controller restart. This chooses duplicate prevention over speculative recovery.

## Alternatives

- Unrestricted UUID + in-memory TTL: once evicted, replay creates again; after restart it also silently duplicates. Bounded tombstones cannot solve indefinite reuse. Reject by encoded expiry/instance instead.
- Client-supplied raw process/execution ID: couples caller namespace to runtime handle formats and still needs fingerprint/claim/scope checks.
- Request ID/tracing, command inventory, or SSE resume: none proves which immutable caller attempt owns a handle lost before persistence. #1309 and #507 remain separate, unmerged work at the inspected baseline.
- Persistent SQL/WAL: recording before or after an OS spawn still leaves an uncertain crash window, unless a durable process supervisor participates. This is outside this bounded runtime-local contract.
- Cache the HTTP response or use a handler-global map: retains unnecessary response data, splits ownership from the runtime lifecycle, and cannot fix PTY terminal relaunch.
- Transparent command SSE retry: risks treating a recovered create as an execution-complete event and conflates creation with output replay. Explicit JSON methods avoid that ambiguity.

## Infrastructure Needed

No service, database, queue or new production dependency. Use existing Go runtime state, SDK transports, OpenAPI generators, and the repository test environments.

## Upgrade & Migration Strategy

This draft needs maintainer review before any upstream implementation claim. The issue has no assignee; GodBlf expressed willingness to help, but the inspected timeline/search contains no linked design/implementation. The useful contribution is this reviewable contract and acceptance evidence, which can be coordinated into an agreed implementation slice.

Deploy supporting execd before using the new opt-in methods. Callers preserve their saved identity and request across restarts. 409 instance mismatch or 410 expiry requires application reconciliation; refreshing an identity is not an automatic migration strategy. Existing callers require no changes.

The separate create paths also fail safely against older execd versions: unsupported
paths return an error instead of silently ignoring a new field and executing. They
avoid changing the generated Kotlin return type of the existing `runCommand` API.
Do not send `operation_id` to the legacy paths and assume recovery is enabled.
