# Round 2 probe evidence

Snapshot: `2a7685ab35dd562ccfaa0e4d47285fdba4cc167e254a594c0b94eee7a51f9b7b` from [the round-2 manifest](../73da315-loop-r2-coverage.json), on uncommitted `dev` based on `73da315fc9d384595c2bf229a37abf3963fcfb82`.

The parent ran these probes with temporary Go overlays. They demonstrate the observed discrepancies; their passing tests mean that the finding was reproduced, not that it was repaired. Production source and the review snapshot were unchanged. No authenticated inference, installed-service changes, publishing or unrelated-process signaling occurred.

| Receipt | Probe and boundary |
| --- | --- |
| `aborted-idle.log` | The existing Copilot fixture emitted partial root text followed by aborted:true idle, before or after the Send acknowledgement. Both buffered/SSE paths returned success; SSE included a successful finish and DONE. Four cases passed. This establishes behavior for the fixture's valid protocol sequence, not its occurrence in an authenticated installed runtime. |
| `acceptance-ownership.log` | A private shell child received a real group TERM and exited. The Go 1.27.3 cancellation callback continued group probes. Mocked probes represented a still-existing/reused group; the test intercepted the final KILL and confirmed through WNOWAIT/ECHILD that the leader had already been reaped. No actual PID reuse or unrelated termination was performed. |
| `case-variant.log` | A request with canonical model=codex followed by undeclared MODEL=copilot was rejected by the strict schema, but the real HTTP handler returned 200 and selected a fake Copilot backend. No provider executable was used. |
| `log-latency.log` | The real readable-log formatter converted total duration to 2s but retained first-text latency as the numeric 1500000000 or 0. No journal/service operation was used. |

The `.go.txt` inputs retain the probe code. `aborted-fixture.patch` modifies only fixture event emission. `acceptance-signal-hook.patch` redirects group signals/probes to a test hook; the hook forwards TERM to the owned group and never forwards the final KILL. Do not describe this controlled identifier-reuse model as an actual kernel-wrap reproduction.

To repeat a probe, create an overlay mapping an otherwise nonexistent `review_*_test.go` within the appropriate package to its saved `.go.txt` input. For the aborted-event probe also map a private patched copy of `internal/adapters/outbound/copilot/copilot_test.go`; for the ownership probe map a private patched copy of `tools/acceptance/process_unix.go`. Apply the matching patch to the private copy only. Use a private TMPDIR/GOTMPDIR and run from the repository root with `GOCACHE=/tmp/harness-gateway-review-go-cache`:

```text
go test -overlay=PRIVATE/overlay.json ./internal/adapters/outbound/copilot -run '^TestReviewAbortedIdleReportsSuccess$' -count=1 -v
go test -overlay=PRIVATE/overlay.json ./tools/acceptance -run '^TestReviewCancellationSignalsAfterLeaderReaped$' -count=1 -v -timeout=20s
go test -overlay=PRIVATE/overlay.json ./internal/adapters/inbound/openai -run '^TestReviewCaseVariantRoutesDespiteSchemaRejection$' -count=1 -v
go test -overlay=PRIVATE/overlay.json ./cmd/harness-gateway -run '^TestReviewFirstTextLogFormatting$' -count=1 -v
```

Each command requires its own corresponding overlay. No normal-source fixes or permanent regression tests were applied in this round.
