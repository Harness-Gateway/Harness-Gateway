# Goldfish Annotated Review Report

Status: resolved

All eight proposed fixes were accepted and applied. See the [implementation and validation record](73da315-loop-resolution.md). A second review round has not run.

**Target:** [entire-repository baseline](73da315-loop-baseline.md), full file and all 159 tracked files in its [coverage manifest](73da315-loop-coverage.json).

**Source:** `73da315fc9d384595c2bf229a37abf3963fcfb82` on `dev` (v0.1.12). Review date: 2026-11-09, Europe/Rome.

## Outcome and evidence boundary

Six requested fresh-context reviewers completed: API/HTTP, providers, process lifecycle, CLI/service, delivery, and documentation/examples. Each read its installed persona, baseline, custom source-oriented standard, required context, and assigned first-party files. Parent conversation turns were excluded using `fork_turns: none`; exclusion of ambient host/project instructions is unverified. Reviewers performed static inspection only and returned their reports before consolidation. No reviewer failed or needed a retry.

The original review produced **eight findings: one conditional Blocker, five Majors, and two Nits**. All received PROPOSE FIX and have now been accepted/applied; there are no contested, escalated, declined or settled items. Original reviewer severities are retained below as the baseline review record. Original affected line numbers refer to the pre-fix baseline. The conditional Blocker did not establish current-host exposure. Actual kernel PID wrap/reuse was not reproduced. No additional issue was established by the documentation/examples reviewer.

The round is **not clean**, with zero consecutive clean rounds; convergence is not established. A further round requires explicit confirmation after resolution. The normal cap remains five rounds.

Coverage is complete for the 157 other tracked files. The 3.8 MB vendored Scalar bundle received integration/provenance inspection, not exhaustive third-party implementation review. The 136,346-line decompressed upstream OpenAPI fixture received checksum/provenance and complete projection-relevant schema inspection, not a full upstream specification audit. These limits are recorded per file in the coverage manifest. The manifest's original compressed-byte newline count has been distinguished from logical decompressed line bounds. This review does not prove the absence of hidden defects.

## Prioritized findings and proposed corrections

### cli-r1-2 — codebase_details — Blocker: localhost resolution can escape loopback validation

- **Affected scope:** [main.go](../../cmd/harness-gateway/main.go#L224), listener binding and `loopbackAddress` at lines 254–262; [doctor.go](../../cmd/harness-gateway/doctor.go#L164), URL validation and dialing; [security.go](../../internal/adapters/inbound/openai/security.go#L39), accepted Host values.
- **Discrepancy:** The literal name `localhost` passes validation, but binding and diagnostic HTTP dialing use ordinary resolver results without checking the actual IP. A hosts/NSS mapping to a non-loopback local interface could expose the unauthenticated listener there. A remote client could supply an accepted `Host: localhost:PORT` header.
- **Rationale:** The supported configuration promises a loopback-only listener and diagnostics restricted to loopback destinations. Validating a hostname's spelling does not enforce the resolved-address boundary.
- **Evidence:** Static conditional security defect. A parent-run process-local libc resolver fixture mapped `localhost` to the unassigned TEST-NET address `192.0.2.1`; the released candidate attempted `listen tcp 192.0.2.1:0`, then failed with EADDRNOTAVAIL. Nonlocal binding was disabled and the address was verified unassigned. No API listener was served, no host resolver files were changed, and no actual network exposure was demonstrated. The current host's ordinary resolver was not alleged to be unsafe. See [receipt](73da315-loop-repros/localhost-resolution.json).
- **AI suggestion: PROPOSE FIX.** Pin accepted localhost inputs to numeric loopback addresses or resolve and validate every selected address without an unchecked second resolution. Validate the bound listener before serving. Apply the same rule to diagnostic dialing.
- **Regression:** Inject a non-loopback resolver result and verify that it cannot produce a served listener or a diagnostic HTTP request. Cover IPv4/IPv6 loopback, normal localhost operation, explicit numeric non-loopback rejection, and port handling. Do not open an unauthenticated network listener in the test.

### providers-r1-1 — codebase_details — Major: cancellation races SDK client retirement

- **Affected scope:** [client.go](../../internal/adapters/outbound/copilot/client.go#L38), cancellation callback and ForceStop at lines 38–46; `Models` line 70; [copilot.go](../../internal/adapters/outbound/copilot/copilot.go#L64), CreateSession.
- **Discrepancy:** The context callback invokes SDK ForceStop while CreateSession or ListModels may still read the SDK's mutable client field. The adapter's lifecycle mutex does not cover those operations. Pinned SDK v1.0.28 clears `c.client` at client.go:806; reads occur at 824, 1133, 1274, and 2129–2133. ForceStop takes the model-cache mutex after the mutation, so it does not protect the ListModels access.
- **Rationale:** Supported request cancellation must safely unblock and retire operation-owned resources without data races.
- **Evidence:** Parent `-race` reproduction with actual adapter code and fixture CLI. An isolated copy of the pinned SDK added only rendezvous hooks before the competing accesses; those scheduling hooks exposed a ListModels/ForceStop data race. Tracked source and the module cache were unchanged. This establishes the race under the injected schedule, not its frequency in ordinary runs. No nil-pointer crash is claimed. See [race log](73da315-loop-repros/cancellation-race.log), [probe](73da315-loop-repros/race_probe_test.go.txt), and [scheduling-only patch](73da315-loop-repros/sdk-rendezvous.patch).
- **AI suggestion: PROPOSE FIX.** Let cancellation close the owned bridge to unblock I/O. Call ForceStop only after active SDK methods have returned. Apply that ordering consistently to startup failures, normal stop, cancellation, and detach deadlines while retaining error collection and idempotent cleanup.
- **Regression:** Deterministically cancel discovery and session creation at SDK access boundaries under `-race`. Verify prompt return, zero retained SDK consumers/processes, storage removal, recovered admission capacity, bounded blocked writes, and legacy detach behavior.
- **Rejected suspicion:** SDK Start and ForceStop transport mutations share `startStopMux`; that narrower startup race was not established and is not a separate finding.

### delivery-r1-1 — implementation_readiness — Major: installer inherits CDPATH during canonicalization

- **Affected scope:** [setup.sh](../../setup.sh#L41), directory creation/canonicalization at lines 41–42 and GOBIN/PATH use at 47–64.
- **Discrepancy:** With exported `CDPATH=.` and relative `--bin-dir bin`, Bash's `cd` prints the selected absolute directory. Capturing both that output and `pwd -P` yields a newline-containing duplicated path. Other CDPATH entries can select a directory different from the one just created.
- **Rationale:** A supported relative installation directory must identify the requested destination and yield a usable executable/PATH entry.
- **Evidence:** Parent ran the actual setup.sh with a private fixture Go executable and `--no-path`. The script exited successfully while the requested `bin/harness-gateway` did not exist; GOBIN contained two absolute paths separated by a newline. The fixture wrote only under `/tmp`; no real module installation or shell-profile change occurred. See [receipt](73da315-loop-repros/installer-cdpath.json).
- **AI suggestion: PROPOSE FIX.** Clear CDPATH for the canonicalizing `cd`, retaining physical-path and colon validation.
- **Regression:** Run installer fixtures with exported `CDPATH=.`, a relative destination, and a different CDPATH directory containing the same basename. Assert exact GOBIN, requested executable location, successful version check, and the generated PATH entry.

### cli-r1-1 — codebase_details — Major: service diagnostics accept a foreign loaded fragment

- **Affected scope:** [doctor.go](../../cmd/harness-gateway/doctor.go#L111), service inspection; [service_status.go](../../cmd/harness-gateway/service_status.go#L60), loaded properties; [service.go](../../cmd/harness-gateway/service.go#L378), existing manager-selected ownership check.
- **Discrepancy:** doctor checks the marker in the local managed file but never validates that systemd selected that file. A foreign or transient same-name unit can supply active/running state and valid same-version startup metadata, receive probes, and produce a successful diagnostic result.
- **Rationale:** Diagnostics D1 requires a managed service. Existing mutation commands already distinguish the local marker from the manager-selected FragmentPath; read/probe diagnostics omit that guard.
- **Evidence:** Parent replayed the existing fake systemd/journal/loopback fixtures with `FragmentPath=/tmp/hg-review-foreign.service`. doctor returned nil, reported PASS, and made the expected health/model requests. No installed service was inspected or changed by this probe. See [log](73da315-loop-repros/doctor-foreign-fragment.log) and [test-only patch](73da315-loop-repros/doctor-fixture.patch).
- **AI suggestion: PROPOSE FIX.** Validate LoadState and the selected fragment against the managed regular file before accepting metadata, executing recorded CLI paths, or probing HTTP. Reuse or extract the existing check while avoiding a redundant snapshot that introduces another check/use interval.
- **Regression:** Add foreign-fragment, missing-fragment/transient, unloaded, and matching-file cases. Invalid ownership must fail with zero CLI-version and HTTP probes. Preserve current-invocation scoping and intentional service/shell PATH differences.

### delivery-r1-2 — implementation_readiness — Major: nullable enum excludes normal streaming values

- **Affected scope:** [main.go](../../tools/project-openapi/main.go#L235), enum normalization at lines 235–242; [main_test.go](../../tools/project-openapi/main_test.go#L77), expected null removal; generated [openapi.json](../../api/openapi.json#L165), ChunkChoice.finish_reason.
- **Discrepancy:** Normalization marks finish_reason nullable but removes null from its enum. Initial/text SSE chunks and the bundled example emit finish_reason:null.
- **Rationale:** The published contract should admit ordinary supported responses. In [OpenAPI 3.0.3 Schema Object semantics](https://spec.openapis.org/oas/v3.0.3.html#fixed-fields-20), nullable expands the type constraint; other constraints, including enum membership, still apply.
- **Evidence:** Static contract mismatch plus parent strict enum check: expand the nullable type as specified and run the enum through Draft4 validation; null is rejected. See [receipt](73da315-loop-repros/schema-null-enum.json). This is not a demonstrated failure in a particular external SDK. The locally pinned kin-openapi validator returns early for nullable nulls before enum checking, so its passing result does not establish strict interoperability.
- **AI suggestion: PROPOSE FIX.** Preserve null in nullable enums, update the normalization expectation, regenerate the OpenAPI document and types using the pinned tools, and validate actual initial/text/terminal chunks. Do not hand-edit generated code. If the generator cannot consume a standards-correct nullable enum, address its input/override behavior without publishing a contradictory schema.
- **Regression:** Enforce both nullable type and enum membership, test valid null and supported finish values, reject unsupported values, and retain include_obfuscation false/null/true assertions.

### process-r1-1 — design_critic — Major: Unix group cleanup retains a reusable numeric identity

- **Affected scope:** [process_unix.go](../../internal/process/process_unix.go#L19), saved PID and kill(-pid); [process.go](../../internal/process/process.go#L12), cleanup contract; callers in [Codex transport](../../internal/adapters/outbound/codex/transport.go#L66), [Copilot bridge](../../internal/adapters/outbound/copilot/bridge.go#L88), and [version probing](../../cmd/harness-gateway/versions.go#L71).
- **Discrepancy:** Callers can reap the group leader before the first group terminator runs. If that group disappears and the numeric PID is recycled into an unrelated process group before cleanup, kill(-pid, SIGKILL) can target the replacement group. Idempotence and ESRCH handling do not authenticate the identity used by the first signal.
- **Rationale:** Owned-resource cleanup must not transfer ownership to unrelated processes through identifier reuse.
- **Evidence:** Static proof of reap-before-signal ordering and numeric targeting. Actual PID allocation wrap/reuse within the interval is plausible but **unverified**; no unrelated process was signaled, and no current-host leakage/destruction is claimed. This is distinct from the documented setsid/setpgid containment limit.
- **AI suggestion: PROPOSE FIX.** Redesign the owned process lifecycle so group termination is issued while the leader/group identity remains reserved, then reap it. Do not substitute a liveness probe that introduces the same check/use race. Resolve portable Unix implementation details before changing the public internal helper contract and all callers.
- **Regression:** Use an isolated, controlled PID namespace or a deterministic process-lifecycle abstraction to exercise exit, delayed cleanup, and numeric identity reuse; verify an unrelated replacement group survives. Cover normal exit, cancellation, inherited pipes, and idempotence. A mocked ordering test alone does not prove kernel-level behavior.

### api-r1-1 — codebase_details — Nit: optional request nullability differs from decoding

- **Affected scope:** [openapi.json](../../api/openapi.json#L90), reasoning_effort/stream_options/include_usage; generated pointer fields; [handler.go](../../internal/adapters/inbound/openai/handler.go#L288), validation.
- **Discrepancy:** Explicit null for these optional fields decodes to nil and is treated as omission, although their published schemas exclude null.
- **Rationale:** Schema consumers and the HTTP handler should agree about accepted parameter forms. This is a contract consistency nit, not a demonstrated operational failure.
- **Evidence:** Static proof; the parent did not run an additional null-request fixture.
- **AI suggestion: PROPOSE FIX.** Preserve current null-as-omitted behavior and encode it explicitly in the projection for these fields. Regenerate instead of editing generated artifacts.
- **Regression:** Compare omitted, null, valid non-null and invalid values against actual handler behavior and schema validation; retain the special obfuscation constraints.

### api-r1-2 — codebase_details — Nit: health 403 description overstates origin restrictions

- **Affected scope:** [contract.go](../../tools/project-openapi/contract.go#L101), shared 403 description; generated [openapi.json](../../api/openapi.json#L472), health response documentation.
- **Discrepancy:** The shared description says cross-origin browser requests are forbidden. Health intentionally permits cross-origin safe reads; the extra unsafe-method check applies to resource-consuming model discovery.
- **Rationale:** Operation-specific documentation should accurately describe the implemented access rule.
- **Evidence:** Static proof plus existing security_test.go:155–165; the full baseline suite passed.
- **AI suggestion: PROPOSE FIX.** Give health a 403 description specific to non-loopback Host rejection. Keep its existing behavior.
- **Regression:** Assert health-specific generated wording and retain cross-origin health/model policy tests.

## Baseline validation and execution limits

Parent verification at the unchanged source baseline:

- gofmt inspection: clean.
- `go vet ./...`: passed.
- `go test ./...`: passed with fixture socket/process access. The initial sandbox-only run failed because loopback sockets were denied; that failure is environmental and is not counted as a project defect.
- `go build ./...`: passed.
- Local Markdown file-link destinations: no missing targets found by the parent and documentation reviewer.
- Baseline source hashes matched before dispatch and reconciliation. Accepted fixes subsequently changed those files; their final hashes are in the resolution evidence's implementation snapshot.

The ordinary suite passing does not contradict the newly instrumented cancellation reproduction or establish absence of the other conditional gaps. The schema probe uses specification-derived type expansion, not a downstream production SDK. No new authenticated inference, Windows/macOS runtime run, installed-service change, commit, push, or release was performed for this review. Previously accepted authenticated Windows and reboot/pre-login qualification limits remain separate from these findings.

## Resolution checkpoint

The user replied **auto accept**, and all eight accepted code/test/documentation fixes were applied and validated. The detailed outcome and remaining validation limits are in the [resolution record](73da315-loop-resolution.md). Another review round requires explicit confirmation; it is not launched automatically by this report. No finding is considered settled solely because its fix was applied.
