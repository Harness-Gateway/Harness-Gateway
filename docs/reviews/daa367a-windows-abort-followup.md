# Windows abort-regression follow-up

Date: 2026-11-09. Base: `daa367a4981664845316c77427058c1f5778128c`.

The first pushed EGM validation completed with ten successful jobs and one failed Windows Go job. The ordinary Windows suite, installed-gateway smoke, repeated launcher integration and installed-CLI job passed. The batch-entry step failed in `TestAbortedIdleHTTPFailureCleansUpAndRecovers/aborted-pre/stream=true`: the immediate process snapshot still reported the fixture CLI running after its batch shim exited. [Original failed job](https://github.com/Harness-Gateway/Harness-Gateway/actions/runs/42597629926/job/129094070430).

Windows Job Object termination of descendants is asynchronous. Replace the immediate assertion with the same two-second bound and ten-millisecond polling used by the existing descendant-cleanup regression. A surviving CLI or process-inspection error still fails the test. Buffered/SSE abort semantics, acknowledgement orders, consumer release and subsequent admission assertions are retained. Production code is unchanged; neither Windows coverage nor the batch wrappers are skipped.

Local validation after this correction:

- `go fmt ./...`: passed.
- `go vet ./...`: passed.
- `go test ./...`: passed.
- `go test -race -count=1 -run '^TestAbortedIdle' ./internal/adapters/outbound/copilot`: passed.
- `git diff --check`: passed.

Corrected test SHA-256: `d497b31929ecf079990c181de677f891f147756594fb5607289cbad55a5c9e33`.

The follow-up commit's exact-SHA GitHub Actions run provides native Windows confirmation; local Linux tests alone do not establish it. This dated note records the original failure and correction, not a claim that the original failed run became green. The original EGM evidence snapshots retain their pre-follow-up source hashes.
