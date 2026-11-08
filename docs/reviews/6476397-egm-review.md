# Bounded EGM pilot: diagnostics and request observability

Date: 2026-11-08 (Europe/Rome).
Baseline: `6476397885b54ee9345b766274ae196cc3d90b90` on `dev`.
Target: [behavior contract](../designs/diagnostics-observability.md).
Standard: [bounded pilot standard](6476397-review-standard.md).
Pilot result before committing/pushing the fixes: initial reviews and the single
fresh follow-up completed; confirmed findings fixed locally. Local validation
passed. Windows runtime and release/deployment gates were still open at that
point; query the corrected commit's CI for current status.

## Installation and execution evidence

Codex reported `egm@egm` version 0.1.1 installed and enabled, from the marketplace
whose recorded Git source is `https://github.com/google/elephant-goldfish-model.git`.
`codex plugin add egm@egm` succeeded and returned installed root
`/home/maurizio/.codex/plugins/cache/egm/egm/0.1.1`.
The shorthand marketplace-add command found a pre-existing registration under
the same name and declined to replace it. Its recorded source was verified;
the existing registration was retained.

This pilot used the installed personas and Codex fresh-context guidance. It did
not run the stock five-stage `egm-review` or claim its multi-round convergence.
The user approved a focused review and at most one fresh follow-up. At pilot
completion, reports and source fixes remained local; installation did not publish
project changes.

| Reviewer | Native task | Persona | Result |
| --- | --- | --- | --- |
| HTTP/lifecycle | `/root/egm_http` | `agents/design_critic.md` | completed |
| Diagnostics/service | `/root/egm_doctor` | `agents/codebase_details.md` | completed |
| Acceptance/readiness | `/root/egm_acceptance` | `agents/implementation_readiness.md` | completed |

All three were spawned with `fork_turns: "none"` and returned actual reports.
All reported reading their installed persona, the complete target and custom
standard, and their mandatory source reading lists. They used static, read-only
inspection; they did not run tests or modify files. No missing reviewer is
counted as clean.
They reported no history/memory retrieval. Parent-turn isolation does not prove
that shared host/project instruction files were excluded.

## Findings and dispositions

All source line references in the finding descriptions refer to the baseline
commit, before local fixes. Current files may have shifted line numbers.

### F1: Recorded failure replaced by later cancellation

Sources: HTTP-1 and ACCEPT-1. Severity: Major. Requirement: O2.
Baseline source: `internal/adapters/inbound/openai/observe.go:28-37,97-100`.
Disposition: PROPOSE FIX, implemented locally.

A failing response write/flush records `write_error`, but deferred context
inspection overwrites that outcome with `canceled` or `shutdown`. A timeout
response can similarly lose its timeout classification during delivery.
Two independent reviewers identified this statically.

The parent reproduced it before changing production code: deterministic writers
canceled the request during write/flush failure or timeout-response delivery.
The logs contained contradictory `outcome=canceled, code=response_write_failed`
and `outcome=canceled, code=harness_timeout` combinations. Shutdown replaced both
the already selected outcome and code.

Fix: context inspection is a fallback only while the outcome remains success.
Transport failures still take precedence if sending a selected error response
fails. `TestRecordedOutcomeSurvivesContextCancellation` covers both cancellation
and shutdown during writes, flushes, and timeout-error delivery.

### F2: Inference probe accepts a non-assistant message

Sources: DOCTOR-1 and ACCEPT-2. Severity: Major. Requirement: D2.
Baseline source: `cmd/harness-gateway/doctor.go:203-212`.
Disposition: PROPOSE FIX, implemented locally.

The probe accepted a completion-shaped object with a user message or missing
role. The original positive HTTP fixture also omitted the assistant role.
The parent reproduced both false passes before the fix.

Fix: use the existing generated `openai.Completion` type and require object
`chat.completion`, a first choice with role `assistant`, and nonempty text.
The contract explicitly describes this minimum check instead of claiming full
schema validation. `TestDoctorProbeRequiresAssistantMessage` rejects wrong and
missing roles; the positive fixture now supplies an assistant message.

### F3: Failed health suppresses independent checks

Sources: DOCTOR-2 and ACCEPT-3. Severity: Major. Requirements: D2/D3.
Baseline source: `cmd/harness-gateway/doctor.go:175-188,200-213`.
Disposition: PROPOSE FIX, implemented locally.

An unhealthy/malformed health response returned before a usable catalog or an
explicitly requested probe could run. The parent reproduced a health 503 with
working model/completion endpoints: neither endpoint was requested.

Fix: retain the health failure while continuing independent checks if the
shared context permits. Invalid URLs and canceled/exhausted contexts remain
real prerequisites. `TestDoctorHealthFailureDoesNotSuppressOtherChecks` checks
the catalog and explicit probe both run once and the overall report still fails.

### F4: CLI version check ignores the selected directory

Source: DOCTOR-3. Severity: Major. Requirement: D1.
Baseline sources: `cmd/harness-gateway/doctor.go:81-94,144-148` and
`cmd/harness-gateway/versions.go:57-61`.
Disposition: PROPOSE FIX, implemented locally.

Diagnostics stat the chosen working directory but run `--version` from the
inspecting process's directory. A directory-sensitive launcher can therefore
fail or select another runtime. The parent reproduced the mismatch with a Go
version fixture that exits 14 outside its selected directory.

Fix: pass the foreground resolved directory or service-recorded directory into
the diagnostic version query and set the command's `Dir`. Startup's existing
query budget and behavior remain unchanged. The new foreground regression and
existing service-invocation test now require the configured directory.

### F5: Zero-duration timings disappear on Windows

Source: parent inspection of completed baseline CI. Severity: Major.
Requirement: O3; baseline code used positive duration as the presence sentinel.
Disposition: PROPOSE FIX, implemented locally.

The [baseline Validate run](https://github.com/Harness-Gateway/Harness-Gateway/actions/runs/42514977948)
completed with ten successful jobs and a failed `go (windows-latest)` job.
`TestCompletionOutcomesAndFirstText` failed for three fast emission cases.
Its recorded summaries had `duration=0` and no `time_to_first_text` field.
This is Windows CI evidence, not a locally reproduced Windows runtime result.

Fix: an optional duration distinguishes absence from a measured zero. Latency
assertions accept nonnegative values. `TestZeroFirstTextLatencyIsStillPresent`
deterministically checks field presence at zero. Windows runtime revalidation
remains pending until a corrected commit runs on Windows.

### N1: Temporal wording for terminal SSE failures

Source: HTTP-2. Severity: Nit. Requirement: O2.
Disposition: PROPOSE FIX, contract clarified locally.

A failure sending usage or `[DONE]` can follow an already written finish frame.
A flush error may occur after bytes have been written. The contract now forbids
starting success frames after a known failure, requires `write_error` for terminal
delivery failures, and acknowledges that written bytes cannot be retracted.

### N2: Missing terminal-frame failure coverage

Source: ACCEPT-4; related to HTTP-2. Severity: Nit. Requirement: O2.
Disposition: PROPOSE FIX, regression coverage added locally.

The original injected failures covered only initial frames. New tests cover
deadline, write, and flush failure at finish, usage, and `[DONE]` frames; assert
HTTP 200 plus `write_error`, no further frame attempt, and capacity recovery.
Those outcome assertions passed without production changes specific to terminal
frames. This is additional coverage, not a demonstrated terminal-frame defect.

## Initial reviewer reading evidence

All paths below are repository-relative unless they identify the installed
plugin persona. Reviewers also read `/home/maurizio/.codex/RTK.md`.

- HTTP reviewer: complete `observe.go`, `handler.go`, `security.go`,
  `observe_test.go`, `models_test.go`, `limits_test.go`,
  `error_classification_test.go` in `internal/adapters/inbound/openai/`;
  complete `internal/application/{gateway.go,models.go}` and
  `internal/domain/completion.go`; selected server shutdown wiring in `main.go`.
- Diagnostics reviewer: complete `cmd/harness-gateway/{doctor.go,
  service_status.go,versions.go,main.go,service.go,doctor_test.go,
  versions_test.go,versions_process_test.go,service_observe_test.go}`;
  complete `internal/buildinfo/info.go`, `internal/process/{process.go,
  process_unix.go,process_other.go,process_windows.go}` and
  `internal/testutil/entrypoint.go`; selected README, console, service fixture,
  generated response types, and outbound launch sites.
- Acceptance reviewer: complete `doctor.go`, `doctor_test.go`, `versions.go`,
  and `service_status.go` under `cmd/harness-gateway/`; complete
  `internal/adapters/inbound/openai/{observe.go,observe_test.go,handler.go,
  models_test.go}`, `internal/application/gateway.go`, and
  `.github/workflows/validate.yml`; selected README, flag/configuration code,
  and transport test infrastructure.

## Verification and remaining gates

Focused pre-fix regressions reproduced F1-F4. After fixes, all focused HTTP and
diagnostic regressions passed. F5 has baseline Windows CI evidence and a local
zero-duration regression. Formatting, vet, build, and all 570 Go tests passed;
the race run also passed all 570 tests.
The corrected built command passed real `doctor --service` checks and discovered
25 model IDs/aliases without inference. All 16 browser cases passed with installed
Chrome. Spring AI fixture acceptance passed 61 tests with no failures or skips.

Retained local artifacts:

- Corrected executable: `/tmp/harness-gateway-egm-pilot`.
- Spring AI diagnostics:
  `/tmp/harness-gateway-egm-acceptance/harness-gateway-acceptance-logs-2703191975`.
- Browser report/screenshots: `/tmp/harness-gateway-egm-browser`.
- Baseline Windows CI failure log: `/tmp/harness-gateway-6476397-ci-failures.log`.

These are temporary local artifacts, not published release receipts. The
authenticated inference option was not exercised against real providers.

The [single fresh follow-up](6476397-egm-followup.md) returned completed and found
no remaining or newly introduced Blocker/Major within this scope. It checked
each F1-F5 and N1/N2 disposition against source and tests, without executing them.

At pilot completion, remaining work was committing/pushing these fixes, green
CI on the corrected commit including Windows, authenticated release-candidate
inference, release/version preparation, public installation verification, and
installed service upgrade/restart acceptance. This report records the pilot's
evidence snapshot; current branch, CI, release, and deployment state must be
checked live. The review itself does not satisfy those operational gates.
