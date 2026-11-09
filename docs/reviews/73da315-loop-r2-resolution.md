# Round 2 accepted-fix resolution

Status: resolved

The user accepted both Majors and the latency-formatting Nit on 2026-11-09, and explicitly accepted the JSON field-case compatibility limit. The changes are applied to the uncommitted `dev` working tree based on `73da315fc9d384595c2bf229a37abf3963fcfb82`. No commit, push, installed-service change, authenticated inference, or release was performed.

## Changes and regressions

| Finding | Resolution | Coverage |
| --- | --- | --- |
| providers-r2-1 | Treat root idle with aborted:true as a backend error before setting the successful-idle flag. Preserve false/omitted success and distinct HTTP cancellation handling. | `TestAbortedIdleFailsBothAcknowledgementOrders` covers before/after Send acknowledgement and false/omitted Aborted. `TestAbortedIdleHTTPFailureCleansUpAndRecovers` covers buffered 502, partial SSE error without a successful finish/DONE, SDK-consumer/process cleanup and subsequent admission. Both abort modes are included in `TestRequestStateRemovedOnEveryExit`. |
| delivery-r2-retry-1 | Acceptance uses the shared Child owner for commands and gateways. Its context callback requests TERM with a six-second grace; the owner serializes graceful/final signaling, keeps the leader unreaped through cleanup, then reaps. Gateway interruption/forced cleanup uses the owner too. Remove acceptance's raw numeric group signals/probes and competing Cmd.Cancel watcher. Late signals after retirement or observed ownership loss fail closed. | `TestCancellationRetainsExitedLeaderThroughGrace` observes the real exited leader with WNOWAIT while cancellation is pending and checks late-signal rejection. `TestGracefulStopReservesIdentityThroughPendingSignals` pauses the signal boundary, verifies reserved kernel identity, and models a replacement signaler after retirement without touching another group. `TestGracefulStopDoesNotSignalAfterObservedOwnershipLoss` models a detected external reaper during cleanup. Existing gated-CLI graceful cancellation, forced-unresponsive-group, startup, exit-status and early-gateway-exit tests pass. |
| cli-r2-1 | Convert time_to_first_text alongside duration and timeout in readable logs. | `TestReadableFirstTextLatency` checks 1.5s, 0s, already formatted text records, and an unrelated numeric count. |
| api-r2-1 | Accepted compatibility limit; retain Go's known-field case-insensitive decoding. Canonical schema names remain unchanged. Document case aliases, later scalar replacement and the schema mismatch; advise canonical names and no duplicate/case-variant members. | `TestKnownFieldCaseAliasesAreAcceptedCompatibilityLimit` records top-level/nested/text-part aliases, stream-option aliases and canonical-plus-case-variant model selection. It explicitly demonstrates schema rejection versus accepted handler behavior, rather than claiming exact schema parity. |

The lifecycle owner joins the first cleanup request: if that request chooses graceful termination, concurrent Stop/Wait calls wait for its bounded grace and termination. The grace path does not reap the leader, and signal methods cannot run after retirement. A known lost-ownership state is checked again before the final forced signal. Existing normal Stop users retain immediate termination behavior. Acceptance keeps its TERM-first window so Java-owned gateways can cancel separately grouped CLIs before forced group cleanup.

The README and Unreleased changelog describe these changes. The JSON case limit is settled as an explicitly accepted risk/compatibility choice, not a silently ignored finding or a schema-enforcement fix.

## Validation

- `go fmt ./...`, `go vet ./...`, `go test ./...`, and `go build ./...`: passed.
- `go test -race -p 1 ./...`: passed.
- Full Windows amd64 and macOS amd64 test-package cross-compilation: passed.
- Spring AI acceptance through the updated runner against a private fixed gateway: **61 tests, zero failures/errors/skips**. No live/account-backed inference was requested. The runner reported owned gateways stopped.
- Parent self-review covered cleanup synchronization, all acceptance startup/wait/stop paths, abort acknowledgement ordering, failure transport semantics, storage/process/consumer cleanup, accepted input compatibility and scope preservation. `git diff --check` and final formatting inspection passed.

The first full ordinary run reached the host's per-user `/tmp` quota while the installer regression wrote the task Go cache. That failed attempt was not treated as green. The task-owned cache was relocated to workspace storage behind its existing GOCACHE path; the full ordinary/race/vet/build gates then passed. Validation used private workspace TMPDIR/GOTMPDIR. Task-only caches, test binaries and scratch files were removed afterward; saved evidence remains in [fixed round-2 receipts](73da315-loop-r2-repros/fixed/).

## Evidence limits and handoff

The ownership regressions prove leader retention and rejection of late/known-unowned signals with real Linux wait-state checks and controlled signal hooks. They do not force actual PID allocation wrap or terminate an unrelated group. The aborted-event cases use the pinned SDK plus deterministic CLI fixtures; they do not establish the frequency of aborted events from authenticated installed runtimes.

Windows/macOS native execution and new remote CI remain pending for this uncommitted patch. Browser acceptance and authenticated/provider/service/public-install checks were not rerun in this resolution; their earlier records retain their original source/proof boundaries. No API-generation change was required by round 2. The accepted authenticated-Windows, reboot/pre-login, and third-party exhaustive-review limits remain unchanged.

Counts: **3 accepted fixes applied, 1 compatibility limit explicitly accepted, 0 contested, 0 escalated, 0 unapplied requested fixes**. `api-r2-1` is settled for subsequent rounds at the same scope; the three applied fixes may be reopened if a fresh reviewer finds the same issue persists. Round 2 remains not clean based on its findings before repairs; zero consecutive clean rounds and no convergence claim.

A fresh round 3 requires explicit user confirmation. No commit/push/install/release is authorized automatically by this resolution.
