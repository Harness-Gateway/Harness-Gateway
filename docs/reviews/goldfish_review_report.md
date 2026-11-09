# Goldfish Annotated Review Report

Status: resolved

All three accepted fixes are applied and validated. See the [round-3 resolution](73da315-loop-r3-resolution.md). Original pre-fix findings, severities and affected ranges are retained below; the reviewed snapshot is historical.

**Target:** entire repository, full-file scope for the 236 files in the [frozen coverage manifest](73da315-loop-r3-coverage.json). [Baseline/line bounds](73da315-loop-r3-baseline.md); [source-oriented standard](73da315-loop-r3-standard.md).

**Source:** uncommitted dev based on `73da315fc9d384595c2bf229a37abf3963fcfb82`, snapshot SHA-256 `be5f7f63d85065876f8691c6c261e7bd06513f64d3fc99cf9ea5829c6c7a2ceb`. Earlier accepted fixes remain present. This is not the clean published tag. The resolved [round-2 report](73da315-loop-r2-report.md) is preserved separately.

## Outcome and completeness

**2 Majors, 1 Nit; no Blockers.** All three are proposed for repair and have bounded Linux parent reproductions. No production source or regression tests changed during this round. No prior fix was found to leave its original reviewed trigger unfixed; the new command-supervision issue concerns independently observed leader exit rather than the fixed cancellation-after-reaping race.

Round 3 is **not clean**, decided before repairs. Consecutive clean rounds: **0**. Convergence requires two consecutive clean rounds and has not been reached. This is round 3 of the normal five-round cap. No churn was established: reverting the previous cancellation ownership fix would not provide the missing natural-exit cleanup policy; the browser/doctor gaps are separate paths.

| Effective area | Persona | Status | Owned coverage | Independent initial findings |
| --- | --- | --- | --- | --- |
| API/HTTP | codebase_details | completed | 30 full files + Scalar exception | none |
| Providers | codebase_details | completed | 27 full files | none |
| Process lifecycle | design_critic | completed | 16 full files | 1 Major |
| CLI/service | codebase_details | completed | 23 full files | 1 Nit |
| Delivery, fresh retry | implementation_readiness | completed | 32 full files + upstream exception | none; browser candidate agreed in post-review adjudication |
| Docs/examples, fresh retry | comprehension | completed | 106 full files | none |

All effective reviewers returned actual final reports. [Report transcriptions](73da315-loop-r3-reviewers/) and [execution receipts, exact full-read paths and attempt statuses](73da315-loop-r3-execution.json) preserve their coverage and limits. **234 files fully read; two explicit third-party exceptions.** Scalar's full minified implementation and the complete upstream OpenAPI semantics were not exhaustively audited. Their provenance/checksums/integration and the projection's upstream schema objects/ancestors were inspected. Generated first-party schemas/types were read fully.

Isolation incident: the parent added completion bookkeeping to the mandatory manifest during active reviews. The first docs attempt saw API/provider receipts and findings arrays; the first delivery attempt saw completion statuses only, without substantive findings. Both were excluded as effective independent audits and retried once fresh after freezing the input and moving bookkeeping to a separate execution record. The CLI auditor confirmed its actual read contained only initial pending statuses and the prior accepted limit. No discarded conclusions were supplied to retries. The first delivery attempt's browser candidate was retained for factual validation: the parent reproduced it and the effective retry agreed in an explicit non-independent post-review adjudication. Its absence from the retry's initial discovery list does not refute that evidence.

Reviewers used `fork_turns: none` and read-only source access without parent transcript. Ambient host/project instruction exclusion remains unverified. Reviewers did not build/test/invoke providers/contact network services/modify Git or services/retrieve history or memory. Historical reports were assessed as dated evidence.

## Prioritized Findings & AI Suggestions

### process-lifecycle-r3-1 [design_critic] — Severity: Major

- **Affected scope:** [tools/acceptance/main.go:75–79](../../tools/acceptance/main.go#L75), [internal/process/process_unix.go:25–55](../../internal/process/process_unix.go#L25), compared with [tools/acceptance/process_unix.go:13–16](../../tools/acceptance/process_unix.go#L13).
- **Discrepancy:** Acceptance applies TERM/grace to context cancellation, but independently observed leader exit enters Child.Wait and immediate group SIGKILL. Java-owned gateways share the command group while provider CLIs have their own groups. Killing gateways immediately prevents their shutdown handlers from retiring gated CLI children.
- **Trigger/impact:** Maven's leader exits successfully or with failure while a Java-owned gateway has an active CLI operation and runner context remains live. Fixture CLIs can remain running after owner Wait and runner failure/completion. Installed-provider behavior after losing its gateway is provider-dependent; no installed-CLI orphan is claimed.
- **Rationale/contract:** Acceptance owns disposable gateways/fixtures and explicitly acknowledges separately grouped CLI children. Natural exit needs the same opportunity for gateway shutdown as cancellation.
- **Evidence category:** Reviewer static proof; parent runtime reproduction with actual gateway and Codex/Copilot catalog-gate binaries. All four combinations (both providers, leader exit 0/7) left a CLI running with live context after owner Wait. Emergency fixture cleanup followed. [Log](73da315-loop-r3-repros/probe.log), [overlay source](73da315-loop-r3-repros/probe_test.go.txt), [receipt](73da315-loop-r3-repros/validation.json).
- ***AI suggestion: PROPOSE FIX.***
- ***Justification:*** Valid cleanup-policy gap distinct from the fixed numeric group-ownership race. Preserve identity reservation; avoid raw post-reaping signals.
- ***Proposed edit:*** Give acceptance command supervision an explicit exit-cleanup policy: after observing exit without reaping, TERM remaining owned group members, allow bounded grace, force termination, then reap. Apply this to Maven/command supervision without imposing grace on ordinary provider-operation cleanup. Keep cancellation idempotent and join its cleanup. Preserve original command status/error.
- **Regression:** Independent leader exit 0/7 with live context, both providers, gateway/CLI PID retirement and command result preserved. Retain cancellation, unresponsive-group, normal-command, identity-retention and late-signal-rejection coverage.

### delivery-r3-1 [implementation_readiness; parent validated] — Severity: Major

- **Affected scope:** [tools/browser/acceptance.mjs:26–36](../../tools/browser/acceptance.mjs#L26), [239–254](../../tools/browser/acceptance.mjs#L239).
- **Discrepancy:** Node owns a gateway but registers no SIGINT/SIGTERM cleanup handler. Normal JavaScript finally handles completion/errors; default terminating signals do not unwind through it. The idle gateway has no parent-exit shutdown policy.
- **Trigger/impact:** Send SIGTERM specifically to Node after readiness. Node dies while its gateway can remain listening, temporary binaries remain, and acceptance.json/gateway.log publication is skipped. Terminal group SIGINT and external supervisors killing the entire tree are different cases; not every CI cancellation is claimed to leak.
- **Rationale/contract:** Delivery tooling must retire owned resources on ordinary cancellation and retain diagnostic evidence.
- **Evidence category:** First delivery attempt's static candidate, parent fixture reproduction and effective retry's agreeing post-review adjudication. The actual unchanged script ran with a real gateway; a loader replaced Playwright only with a controlled launch pause after readiness. Node exited from SIGTERM, gateway/runtime remained, result receipt was absent. Supervisor then stopped gateway/group. No browser/UI run claimed. [Proof](73da315-loop-r3-repros/browser-proof.json), [supervisor](73da315-loop-r3-repros/browser-probe.py), [reviewer record](73da315-loop-r3-reviewers/delivery.md).
- ***AI suggestion: PROPOSE FIX.***
- ***Justification:*** Directly reproduced resource-ownership failure; ordinary finally is insufficient for targeted termination.
- ***Proposed edit:*** Install signal handling before spawning owned processes. Route normal exit, errors and SIGINT/SIGTERM through one idempotent bounded cleanup operation. Close browser resources, signal/await gateway, force cleanup after a bound, verify fixture retirement, save interruption/failure diagnostics and remove runtime files. Return nonzero on interruption. A cleanup error must not skip subsequent retirement/diagnostic steps.
- **Regression:** Spawn runner, await gateway readiness, target Node PID with SIGTERM; assert bounded runner/gateway exit, runtime removal and interrupted receipt. Repeat during a fixture stream and assert CLI retirement. A controlled Playwright pause can make launch-phase coverage deterministic; retain real-browser normal-path acceptance separately.

### cli-service-r3-1 [codebase_details] — Severity: Nit

- **Affected scope:** [cmd/harness-gateway/doctor.go:89–98](../../cmd/harness-gateway/doctor.go#L89); prepareBackends in [main.go:146–184](../../cmd/harness-gateway/main.go#L146).
- **Discrepancy:** One missing enabled executable makes prepareBackends return nil harnesses and suppresses version queries for available CLIs too.
- **Trigger/impact:** Foreground doctor enables both harnesses with one valid executable and one nonexistent path. HTTP checks continue and overall status correctly fails, but useful independent version diagnosis is missing. Incomplete reporting, not false success.
- **Rationale/contract:** Diagnostics design D1 requires enabled CLI versions; D3 requires independent useful checks when prerequisites permit.
- **Evidence category:** Reviewer static proof; four parent cases across both harness orders and both available CLIs. Each valid fixture works alone; paired with a missing path, query count is zero while HTTP continues and doctor fails. [Log](73da315-loop-r3-repros/doctor-probe.log), [overlay source](73da315-loop-r3-repros/doctor-probe_test.go.txt).
- ***AI suggestion: PROPOSE FIX.***
- ***Justification:*** Small, useful diagnostic correction with deterministic coverage; original Nit severity retained.
- ***Proposed edit:*** Resolve/report foreground diagnostic executables independently after shared prerequisite validation, then query every successfully resolved enabled CLI. Preserve errors for missing CLIs, HTTP continuation and overall failure. Keep startup/service-install fail-fast configuration behavior.
- **Regression:** Valid fixture plus missing path in both orders; valid CLI queried/reported, missing CLI fails, HTTP continues, overall command fails. Service ownership validation remains a prerequisite in service mode.

## Settled compatibility limit

`api-r2-1` remains settled by explicit user acceptance: known-field case-insensitive JSON aliases and later matching scalar-member override can exceed the case-sensitive published schema. README and a differential regression record this limit. It was not reopened; canonical field names remain documented client guidance.

## Validation and evidence boundaries

All 236 frozen hashes matched after effective reviewers completed and before replacing this canonical report. This report is the sole inventoried file changed by post-review bookkeeping; its exact reviewed round-2 contents remain in the byte-identical archived report. Round-3 scaffolding/receipts were created after inventory and are not silently counted as reviewed source.

All 48 files in the previous round-2 fixed-source snapshot still match; every current source change is covered. Its recorded fmt/vet/test/race/build, 61 Spring AI fixtures and Windows/macOS cross-compilation results retain their source bounds. They were not rerun wholesale for unchanged source. New parent probes passed by confirming defects, not fixes. New native Windows/macOS, remote CI, browser UI, authenticated inference, installed-service changes, commit/push/install/release were not performed. Existing accepted deployment/third-party exhaustive-audit limits remain.

## Resolution

The user replied auto accept. Both Majors and the diagnostic Nit are applied with regression coverage; see the [resolution record](73da315-loop-r3-resolution.md) and [fixed validation receipt](73da315-loop-r3-repros/fixed/validation.json). Counts: 3 accepted/applied, 0 contested/escalated/declined, 0 unapplied requested fixes. The original [pre-fix report](73da315-loop-r3-report.md) is preserved separately. The JSON field-case compatibility limit remains settled.

Round 3 remains not clean based on its original findings before repairs; zero consecutive clean rounds and no convergence claim. A fresh round 4 requires explicit confirmation. No commit/push/install/release follows automatically, and native Windows/macOS CI remains pending for the uncommitted patch.
