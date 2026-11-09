# Round 1 accepted-fix resolution

Status: resolved

The user accepted all eight PROPOSE FIX items on 2026-11-09. All eight were applied in the `dev` working tree based on `73da315fc9d384595c2bf229a37abf3963fcfb82`. No commit, push, installed-service upgrade, or release was performed in this resolution step.

## Changes and regression coverage

| Finding | Applied correction | Regression/evidence |
| --- | --- | --- |
| cli-r1-2 | Canonicalize localhost to numeric 127.0.0.1 for both binding and diagnostics, and check the actual bound listener. Document explicit IPv6 loopback configuration. | `TestLocalhostUsesNumericLoopback`; existing non-loopback rejection tests; the fixed private candidate binds 127.0.0.1 under the same process-local resolver override that previously produced a TEST-NET bind attempt. |
| providers-r1-1 | Cancellation and detach deadlines close only the owned bridge; SDK ForceStop runs after active SDK calls return. Remove the inadequate snapshot-only mutex. | `TestCancellationDuringActiveSDKCalls` covers in-progress models.list/session.create and consumer cleanup. The same isolated SDK scheduling hooks now pass with retirement postponed until the paused operation is released, under `-race`. Existing initialization, blocked-send, detach and storage cleanup tests also pass. |
| delivery-r1-1 | Clear CDPATH only for installer directory canonicalization. | `TestSetupRelativeDirectoryIgnoresCDPATH` covers CDPATH=. and a different same-named directory, requested executable placement and the resulting PATH entry. |
| cli-r1-1 | Share manager-selected fragment validation with service diagnostics, using properties from the existing inspection before CLI/HTTP probes. | `TestDoctorRejectsForeignLoadedServiceBeforeProbes` covers foreign, transient/missing-fragment and unloaded units, with zero diagnostic CLI/HTTP probes. Existing current-invocation and mutation ownership tests pass. |
| delivery-r1-2 | Preserve null in nullable enums and regenerate the document/types with the pinned tools. | `TestNullableNormalization`; `TestStreamingResponsesSatisfyStrictPublishedContract` validates actual role/text/terminal/usage chunks with JSON Schema type expansion and enum enforcement, and rejects an unsupported finish reason. |
| process-r1-1 | Return a Child lifecycle owner from process.Start. All its production callers use Child.Wait. Linux observes exit with WNOWAIT; macOS registers NOTE_EXIT and checks the unreaped state before reaping; Stop retires the group while the leader reserves its identity. Windows retains Job Object ownership. Unexpected external reaping disables later numeric group signaling. | `TestWaitPreservesKernelIdentityUntilGroupTermination` pauses real cleanup and proves the kernel still holds the exited child at the signal boundary; it then verifies final reaping and repeated Stop/Wait. `TestExternallyReapedChildDoesNotSignalReusedIdentity` verifies fail-closed behavior. Existing containment, startup, cancellation, version and launcher cases pass on Linux. macOS-specific zombie and stopped-child tests cross-compile; native execution remains pending. |
| api-r1-1 | Explicitly document null-as-omitted behavior for reasoning_effort, stream_options and include_usage in the projection. Put object nullability on StreamOptions itself, rather than an ignored OAS 3.0 $ref sibling. | `TestOptionalNullParametersMatchPublishedContract` compares omitted/null/valid/invalid parameter forms against the strict schema and real handler. Existing obfuscation tests pass. |
| api-r1-2 | Give health its own non-loopback-Host 403 description while preserving safe-read origin behavior. | `TestHealthDescriptionMatchesSafeReadPolicy`, generated document comparison, and existing cross-origin health/model tests. |

The strict contract tests use the already pinned Google JSON Schema dependency, promoted from an indirect to a direct test dependency without changing its version. Generated files were produced by the projection tool and oapi-codegen v2.5.1; they were not edited manually. README, the diagnostics contract, and the Unreleased changelog describe the new behavior.

## Completed validation

- `go fmt ./...`, final formatting inspection, `go vet ./...`, `go test ./...`, and `go build ./...`: passed.
- `go test -race -p 1 ./...`: passed.
- Final process/cancellation regression refinements repeated five times under `-race`: passed.
- Pinned OpenAPI document validation and checksum-pinned upstream projection: passed.
- Pinned API generation reproduced the checked-in generated output exactly.
- Full macOS amd64 and Windows amd64 test-package cross-compilation: passed; affected test/platform refinements were recompiled afterward.
- Spring AI acceptance against a private fixed gateway: **61 tests, zero failures/errors/skips**. This includes protocol forwarding, failures, cancellation, capacity recovery and fixture stress. No authenticated inference was requested.
- Browser acceptance with the pinned Playwright headless browser: **16 cases passed**, including Scalar rendering, discovery, buffered/SSE requests, usage, cancellation, capacity recovery, origin checks and mobile rendering.
- The process-local resolver probe against the private fixed gateway: local health passed on numeric loopback despite a localhost-to-TEST-NET override; clean SIGTERM shutdown passed.
- Parent self-review checked production changes, all process.Start callers, generated artifacts, regressions, documentation and scope. `git diff --check` passed.

The first validation attempts encountered the host's `/tmp` per-user quota; temporary fixture writes and cross-compiled outputs failed there. Validation was rerun successfully with both TMPDIR and GOTMPDIR on task-private workspace storage. The browser initially lacked its pinned executable; it was downloaded into task-private storage and all browser cases then passed. These environmental failures are not recorded as successful checks and were not addressed by changing application behavior.

## Evidence and limits

Successful check logs, the instrumented SDK probe, strict fixture results, and the fixed resolver receipt are retained in [resolution evidence](73da315-loop-repros/fixed/). The earlier [review report](73da315-loop-r1-report.md) retains the original findings and pre-fix evidence.

The Linux identity test demonstrates the ownership invariant that prevents reuse before the signal; it does not force actual PID allocation wrap in a kernel namespace. The SDK race probe changes only scheduling in an isolated SDK copy; it does not patch the cached dependency. The macOS implementation follows exit-event semantics documented by [Apple's kernel source](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/kern/kern_event.c) and preserves unreaped identity; Linux WNOWAIT semantics are described in the [wait manual](https://man7.org/linux/man-pages/man2/waitpid.2.html). No native Windows/macOS execution or new remote CI run is claimed for this uncommitted patch. In particular, native macOS execution of the new exit-notification/zombie cases remains an important next CI gate.

The prior authenticated Windows and reboot/pre-login qualification limits remain unchanged. Vendored Scalar and the complete upstream OpenAPI source retain the dependency-inspection limits recorded in the first-round coverage manifest.

## EGM handoff

Resolution counts: **8 accepted/applied, 0 contested, 0 escalated, 0 declined, 0 unapplied accepted fixes**. Fixed findings remain eligible for reopening; they are not settled by being edited. Round 1 remains not clean, because cleanliness was decided from its original findings before fixes. There are zero consecutive clean rounds and no convergence claim.

A fresh second round must re-read the current files and updated full-file bounds, including the added source/tests and resolution artifacts. It requires explicit user confirmation before dispatch. This resolution does not itself authorize committing, pushing, installing or publishing changes.
