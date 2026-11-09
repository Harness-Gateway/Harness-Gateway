# Round 3 accepted-fix resolution

Status: resolved

The user replied **auto accept** on 2026-11-09. All two Majors and the diagnostic Nit are applied with regressions in the uncommitted `dev` working tree based on `73da315fc9d384595c2bf229a37abf3963fcfb82`. Earlier accepted changes were preserved. The [original round-3 report](73da315-loop-r3-report.md) retains pre-fix findings and review evidence.

## Changes and regressions

| Finding | Resolution | Regression evidence |
| --- | --- | --- |
| process-lifecycle-r3-1, Major | Add Child.WaitGracefully: observe direct-child exit without reaping, signal remaining descendants, retain identity through bounded grace/forced cleanup, then reap. Acceptance commands select TERM/six-second grace on independent exit as well as cancellation. Ordinary provider/gateway Wait and Stop keep immediate cleanup. Concurrent wait/stop calls share the first selected policy and result. | TestCommandStopsGatedCLIs covers Codex/Copilot cancellation plus independent leader exits 0/7 with a live runner context, retained command status and gateway/CLI retirement. TestGracefulWaitReservesExitedLeaderAndPreservesStatus checks real Linux WNOWAIT ownership at a paused signal boundary, invalid policies, exit status, repeated wait and late-signal rejection. Existing external-reaper, cancellation, forced-group and startup regressions pass. |
| delivery-r3-1, Major | Install Unix interruption handling before subprocess startup. Run builds asynchronously with cancellation. Route completion, errors and signals through one idempotent cleanup operation. Bound gateway, build and browser settlement; retire the gateway before browser cleanup; isolate cleanup failures so diagnostics and runtime removal still run. Preserve interrupted status and return 130/143. Auxiliary HTTP servers also receive cleanup. | Four Linux subprocess regressions target Node during browser launch, browser-close failure and actual Codex/Copilot held streams. Assert bounded runner/gateway/CLI exit, interrupted receipt, gateway diagnostics and runtime removal. Repeat a signal to check idempotence. Playwright is paused deterministically; gateway/CLI processes and HTTP streams are real fixtures. Final readiness-file publication is atomic. npm test includes these regressions before normal UI acceptance. |
| cli-service-r3-1, Nit | After foreground configuration resolution fails, resolve enabled CLIs independently and query those still available. Report each failed lookup, continue HTTP diagnostics and retain overall failure. Startup/service installation resolution and service-mode ownership prerequisites remain unchanged. | TestDoctorChecksAvailableCLIsIndependently covers both harness orders and each available CLI, verifies its fixture succeeds alone and receives exactly one version query with the missing peer, checks both CLI reports, HTTP continuation and overall failure. |

Unix acceptance commands deliberately retain a fixed six-second cleanup window even after an otherwise successful direct-child exit. This gives remaining Java-owned gateways time to retire separately grouped CLIs while the leader reserves identity. It adds tool-runner latency; provider-operation cleanup does not acquire that grace. No raw post-reaping process-group signaling was introduced.

The Unreleased changelog records all three corrections. The explicitly accepted JSON field-case compatibility limit remains documented and settled; this resolution does not change its parser or schema policy.

## Validation

- `go fmt ./...`, `go vet ./...`, `go test ./...`, `go build ./...`: passed.
- `go test -race -p 1 ./...`: passed.
- Windows amd64 and macOS amd64 cross-compilation: all 15 packages, including 11 test executables per platform, passed. These are compilation checks, not native execution.
- Updated acceptance runner against a private fixed gateway: **61 Spring AI fixture tests**, zero failures/errors/skips; runner returned success and confirmed owned gateways stopped.
- Browser interruption regression: **4 cases** passed with actual local gateways/CLI fixtures and controlled browser pauses.
- Real Chromium acceptance: **16 cases** passed using installed Chromium `153.0.8010.12` through the existing executable override. No off-origin browser requests or UI errors were reported; normal cleanup succeeded.
- Parent self-review and `git diff --check`: passed. Final source hashes cover all 54 changed/new implementation files across the three accepted rounds.

The first targeted Go attempt exposed a bypassed cleanup hook in the wait refactor; restoring the ordinary Child.Stop path fixed it before all final gates. The first browser fixture omitted Codex's required system message; it was corrected before the final signal suite. The initial combined npm run passed its interruption stage but could not locate the expected Chromium build locally; its failed result is not counted as green. The real UI stage was then run successfully with the already installed browser via the supported override. Final signal tests were repeated after making their readiness handshake atomic. [Saved receipts](73da315-loop-r3-repros/fixed/validation.json) distinguish these attempts and the successful commands.

Validation used private workspace TMPDIR/GOTMPDIR/GOCACHE. Task-owned caches, test executables, screenshots and scratch/runtime files were removed after retaining logs, JSON receipts and the [implementation snapshot](73da315-loop-r3-repros/fixed/implementation-snapshot.json).

## Evidence limits and handoff

No native Windows/macOS tests or new remote CI ran for this uncommitted patch. No authenticated inference, installed-service mutation, public installation, commit, push or release occurred. Existing authenticated-Windows/reboot/prelogin qualification limits and third-party exhaustive-audit exceptions retain their previous scope. Real browser acceptance used the installed executable override; the CI-installed default Chromium build remains a remote gate.

Counts: **3 accepted fixes applied, 0 contested, 0 escalated, 0 declined, 0 unapplied requested fixes**. Fixed findings remain eligible for reopening. Round 3 remains not clean based on its original findings before repairs; consecutive clean rounds remain zero, and no convergence or release-readiness claim is made. A fresh round 4 requires explicit user confirmation under the EGM loop checkpoint.
