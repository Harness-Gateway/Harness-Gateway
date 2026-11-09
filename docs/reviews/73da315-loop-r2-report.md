# Goldfish Annotated Review Report — Round 2

Status: resolved

Both Majors and the logging Nit have been applied and validated. The JSON field-case behavior was explicitly accepted as a compatibility limit. See the [round-2 resolution record](73da315-loop-r2-resolution.md).

**Target:** [round-2 baseline](73da315-loop-r2-baseline.md), full-file audit of the 203 files in the [round-2 coverage manifest](73da315-loop-r2-coverage.json).

**Source:** uncommitted `dev` based on `73da315fc9d384595c2bf229a37abf3963fcfb82`. Snapshot SHA-256: `2a7685ab35dd562ccfaa0e4d47285fdba4cc167e254a594c0b94eee7a51f9b7b`. Date: 2026-11-09, Europe/Rome.

The [round-1 report](73da315-loop-r1-report.md) and its [eight-fix resolution](73da315-loop-resolution.md) are preserved separately. No application or regression source changed during the round-2 review itself. The original snapshot remained byte-identical through completion of all effective reviewers; the subsequently accepted repairs are recorded separately in the resolution.

## Outcome

Round 2 found **2 Majors and 2 Nits; no Blockers**. The user approved and received **3 applied fixes and 1 explicitly accepted compatibility limit**. Original severities and suggestions are preserved below as the pre-fix review record. None of the eight round-1 fixes was reopened. The acceptance-tooling issue was in a separate raw-signaling path, now migrated to the shared lifecycle owner.

This round was **not clean** based on its Major findings before repairs. Applying fixes does not turn that original review into a clean round. There are zero consecutive clean rounds; convergence has not been reached. This is round 2 of the normal five-round cap. No churn was established: reverting an earlier fix would not resolve these findings.

### Reviewer completion and isolation

| Area | Effective status | Findings | Limits |
| --- | --- | --- | --- |
| API/HTTP | completed | 1 Nit | Scalar integration/provenance inspected; full minified implementation excluded explicitly. |
| Providers | completed | 1 Major | Relevant pinned SDK paths inspected; installed-runtime reachability not demonstrated. |
| Process lifecycle | completed | none | Static Linux/macOS/Windows inspection; no native runtime execution by reviewer. |
| CLI/service | completed | 1 Nit | Static source/tests; no real service operation by reviewer. |
| Delivery, first attempt | not run for independent-review purposes | excluded | Reviewer accessed a collaboration inventory exposing earlier conclusions after its source reads. The attempt was discarded as an isolated review. Its Go 1.25 supporting-source observation was not used as project-toolchain evidence. |
| Delivery, fresh retry | completed | 1 Major | Independently read the full area, used actual Go 1.27.3 source, and did not access other conclusions. |
| Documentation/examples | completed | none | All historical artifacts and Java example/tests inspected; execution claims assessed against retained evidence. |

All six effective requested areas completed. Fresh parent turns were disabled with `fork_turns: none`; exclusion of ambient host/project instructions is unverified. Reviewers were read-only and did not run tests/providers or change files. The replacement resolved the one failed-isolation attempt; it is not counted as an additional clean reviewer.

All 201 nonexception snapshot files received full assigned reads. The two explicit dependency exceptions are the 3.8 MB Scalar bundle and the 136,346-line decompressed upstream OpenAPI input. Their provenance, integration and relevant schema objects were inspected; their entire implementations/specification were not exhaustively audited. The coverage manifest records these limits. This audit does not prove absence of all hidden defects.

## Prioritized findings and suggestions

### providers-r2-1 — codebase_details — Major: aborted Copilot runs can report success

- **Affected scope:** [copilot.go:198–201](../../internal/adapters/outbound/copilot/copilot.go#L198); acknowledgement handling at lines 153–154; HTTP success conversion in [handler.go:252–274](../../internal/adapters/inbound/openai/handler.go#L252).
- **Discrepancy:** A healthy HTTP request can receive partial root assistant text and then `session.idle` with `aborted:true`, without an earlier session.error. The adapter ignores Aborted and returns nil once Send is acknowledged. Buffered HTTP returns a successful completion; streaming emits a successful finish and DONE.
- **Rationale:** Known provider failures must not become successful completion markers. The pinned SDK v1.0.28 documents SessionIdleData.Aborted as cancellation of the preceding loop and its own legacy completion consumer treats that event as an error (`rpc/zsession_events.go:2339–2343`, `session.go:778–780`).
- **Evidence:** Static SDK/adapter proof and parent fixture reproduction. All four combinations—idle before/after Send acknowledgement, buffered/SSE—returned HTTP 200 with partial text and finish_reason=stop; streaming included DONE. See [probe log](73da315-loop-r2-repros/aborted-idle.log), [fixture-only patch](73da315-loop-r2-repros/aborted-fixture.patch), and [probe source](73da315-loop-r2-repros/aborted_probe_test.go.txt). This proves behavior for the specified valid protocol sequence, not that an authenticated installed runtime has emitted it here.
- **AI suggestion: PROPOSE FIX.** Check `data.Aborted != nil && *data.Aborted` before accepting SessionIdleData as successful. Return a descriptive backend error while preserving distinct HTTP-context cancellation classification. Apply the check before setting idle, so both acknowledgement orderings use it.
- **Proposed edit:** In the SessionIdleData case, insert an abort guard returning `errors.New("copilot session aborted before completion")` before `idle = true`. The file already imports errors.
- **Regression:** Emit partial root text and aborted idle before/after acknowledgement. Require an adapter error, buffered failure, SSE error with no successful finish/DONE, owned storage/process/consumer cleanup, and subsequent admission recovery. Also retain false/omitted Aborted success cases and existing Send-failure/cancellation behavior.

### delivery-r2-retry-1 — implementation_readiness — Major: acceptance cancellation can outlive process-group ownership

- **Affected scope:** [process_unix.go:17–42](../../tools/acceptance/process_unix.go#L17), raw TERM/group probes/final KILL; `killCommand` lines 49–50; [main.go:43–49](../../tools/acceptance/main.go#L43), the command runner's Cmd.Run.
- **Discrepancy:** The acceptance runner's custom Cancel callback keeps a numeric PGID while Cmd.Wait can reap the group leader. If the original group disappears and its ID is reused before a pending TERM/probe/KILL, the callback can target the replacement group. The six-second grace loop does not preserve ownership.
- **Rationale:** Interrupted acceptance must retire only its owned Maven/Java/gateway processes. The production internal/process owner now preserves identity, but this independent acceptance path still uses raw reaping/signaling.
- **Evidence:** The replacement independently established the ordering in actual Go 1.27.3: os/exec/exec.go:935–948 consumes Process.Wait before collecting the cancellation watcher result; watchCtx calls Cancel at 815–830. Parent reproduction used a real private shell child, real group TERM, and WNOWAIT/ECHILD to establish that reaping preceded the final pending KILL. Group probes were mocked to represent a still-existing/reused group and the KILL was intercepted. See [log](73da315-loop-r2-repros/acceptance-ownership.log), [test hook patch](73da315-loop-r2-repros/acceptance-signal-hook.patch), and [probe source](73da315-loop-r2-repros/acceptance_ownership_test.go.txt). **Actual PID reuse and unrelated-process termination were not reproduced.** The source ordering is confirmed; collateral termination remains conditional.
- **AI suggestion: PROPOSE FIX.** Give acceptance a lifecycle owner that retains the unreaped leader through every possible graceful and forced group signal, then reaps it. Keep the TERM-first grace period, allowing Java-owned gateways to cancel their separately grouped CLIs. Prevent any later group signal after ownership is lost or retired.
- **Proposed change scope:** Update acceptance command startup/wait/cancellation and gateway stop paths as needed; reuse or extend internal/process ownership where appropriate. Do not call Cmd.Wait from Cmd.Cancel, do not replace the grace period with immediate group KILL, and do not use a liveness check as an identity substitute.
- **Regression:** Deterministically pause cancellation around exit/reaping, simulate reused IDs without signaling unrelated processes, and assert no numeric group signal after ownership release. Retain graceful gated-CLI cleanup, forced unresponsive-group timeout, normal completion, startup failure and interruption tests. Cover all acceptance paths using killCommand rather than only the Maven wrapper.

### cli-r2-1 — codebase_details — Nit: readable logs leave first-text latency in nanoseconds

- **Affected scope:** [service_logs.go:237–241](../../cmd/harness-gateway/service_logs.go#L237); first-text producer in [observe.go:52–53](../../internal/adapters/inbound/openai/observe.go#L52).
- **Discrepancy:** The formatter humanizes duration and timeout, but not time_to_first_text. A first-text duration of 1.5 seconds renders as 1500000000 alongside duration=2s. Zero renders as 0 rather than a duration string.
- **Rationale:** README promises human-readable durations in readable logs and identifies first-text latency as a duration.
- **Evidence:** Static proof plus the real formatter probe for positive and zero values. See [log](73da315-loop-r2-repros/log-latency.log) and [probe source](73da315-loop-r2-repros/latency_log_test.go.txt). No journal/service operation was performed.
- **AI suggestion: PROPOSE FIX.** This is a small, useful readability correction.
- **Proposed edit:** Add `|| key == "time_to_first_text"` to the existing duration/timeout conversion condition.
- **Regression:** Assert 1.5s and 0s for JSON first-text durations alongside total duration. Preserve already formatted text-log duration values and unrelated numeric fields.

### api-r2-1 — codebase_details — Nit: JSON field-case aliases exceed the published strict schema

- **Affected scope:** [handler.go:119–122](../../internal/adapters/inbound/openai/handler.go#L119), generated tagged fields, and the closed request schema in [openapi.json:66–79](../../api/openapi.json#L66). The decoder behavior also applies to nested tagged fields and text parts.
- **Discrepancy:** Go's struct JSON decoder matches property names case-insensitively. A canonical model=codex followed by undeclared MODEL=copilot routes the request to Copilot instead of rejecting MODEL, although the schema uses exact names and additionalProperties=false.
- **Rationale:** Schema validation and actual input acceptance differ for case variants. The selected harness changes, but a client can already choose either enabled harness; no gateway security-boundary bypass was established.
- **Evidence:** Parent schema/handler differential probe: schema rejected the undeclared property, HTTP returned 200, and the fake Copilot backend was selected. See [log](73da315-loop-r2-repros/case-variant.log) and [probe source](73da315-loop-r2-repros/case_test.go.txt). No provider executable or credentials were used.
- **AI suggestion: DECLINE.** Record this minor permissive-decoding compatibility limit for this pass. Enforcing exact names across every nested/raw-message shape requires a separate validation-policy change with wider scope than its demonstrated impact. This is not a contest: the discrepancy is valid, and exact casing parity must not be claimed.
- **Resolution:** The user explicitly accepted this compatibility limit. README and a differential regression now record it, and the item is settled at this scope. If the policy is deliberately changed later, use case-sensitive validation grounded in generated schema/types, with uppercase-only/case-variant/nested coverage and zero backend calls for rejected requests.

## Reconciliation and limits

The two Majors are new affected paths/conditions, not regressions shown to be caused by the eight prior fixes. The delivery finding uses the independent replacement report; the initial compromised attempt is excluded as fresh-review evidence. The parent also validated its ordering using the correct project toolchain.

A provider reviewer noted an unverified concern about unrelated startup idle events and discarded Send message IDs. Supported installed-runtime reachability was not established, so it is not counted as an additional defect or mandatory change. No claimed deployed exploit, native macOS failure, or authenticated provider abort follows from these probes.

The prior accepted-fix implementation snapshot still matches all 166 implementation/baseline files. Its recorded formatting, vet, build, full unit/race suite, 61 Spring AI fixtures and 16 browser cases were not rerun wholesale during this unchanged-source review. The four isolated parent probes above were executed in round 2. No native Windows/macOS execution, new authenticated inference, installed-service change, commit, push or release is claimed. Previously documented platform/deployment and dependency-inspection limits remain.

## Resolution checkpoint

The user replied Auto accept and specifically requested both Major fixes, the logging fix, regressions, and recording the field-case limit. All are completed in the [resolution record](73da315-loop-r2-resolution.md). Another round requires explicit confirmation. Neither convergence nor release readiness is established by this round, and no commit/push/install/release follows automatically.
