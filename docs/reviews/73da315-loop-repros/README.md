# Round 1 reproduction evidence

Baseline: `73da315fc9d384595c2bf229a37abf3963fcfb82`.

These are parent-run review probes, not fixes or installed-service acceptance. Tracked production source and cached dependencies remained unchanged. The Go probe inputs were overlaid from `/tmp/hg-egm-r1`.

- `cancellation-race.log`: `go test -race` against the real Copilot adapter and fixture CLI. A private copy of pinned SDK v1.0.28 received only the rendezvous hooks in `sdk-rendezvous.patch`. The test is retained in `race_probe_test.go.txt`. Both competing accesses are released from one channel, without imposing ordering between them. The race detector reports ListModels reading the SDK client while ForceStop clears it. This is an instrumented schedule, not an ordinary-run frequency measurement.
- `doctor-foreign-fragment.log`: existing doctor fixtures with the test-only changes in `doctor-fixture.patch`. The running case still returns PASS even though systemd's fixture-selected fragment is foreign. No actual service operation occurs.
- `installer-cdpath.json`: actual setup.sh with CDPATH=. and relative --bin-dir, a fixture Go executable, and --no-path. The requested executable is absent, while GOBIN contains a newline. Only private temporary files were created; no real install or shell profile was changed.
- `schema-null-enum.json`: published finish_reason schema, its OAS 3.0.3 nullable type expansion, and Python jsonschema Draft4 enum rejection of null. This proves a specification-level mismatch, not failure in a named production client.
- `localhost-resolution.json`: the released candidate under `GODEBUG=netdns=cgo` with a process-local `getaddrinfo` fixture (`localhost-resolver.c`). It attempts to bind the fixture's non-loopback TEST-NET address. The address was checked unassigned and nonlocal binding disabled, so binding fails safely; no API was exposed. No system resolver configuration was changed.
- `baseline-tests.log`, `baseline-vet.log`, `baseline-build.log`: successful unmodified-source baseline checks. Empty vet/build output denotes success; exit codes were verified separately by the parent.

## Repeating the Go race probe

Use a private writable directory. Copy the cached `github.com/github/copilot-sdk/go@v1.0.28` there, make its copied client.go writable, and apply sdk-rendezvous.patch with `patch -p1`. Copy the repository go.mod/go.sum to private review.mod/review.sum files and add a module replacement pointing at that SDK copy. Never edit the cached module itself.

Create a Go overlay mapping the absolute, otherwise nonexistent repository path `internal/adapters/outbound/copilot/review_race_probe_test.go` to a private copy of race_probe_test.go.txt. Run from the repository root:

```text
GOCACHE=/tmp/harness-gateway-review-go-cache go test -race -modfile=PRIVATE/review.mod -overlay=PRIVATE/overlay.json ./internal/adapters/outbound/copilot -run '^TestReviewCancellationSDKClientRace$' -count=1 -v
```

The expected baseline result is a race-detector failure. The private test and dependency overlays do not constitute a production fix. Go rejects overlays beneath GOMODCACHE, which is why this probe uses an isolated SDK copy and a private module replacement.

For the doctor probe, apply doctor-fixture.patch to a private copy of cmd/harness-gateway/doctor_test.go and overlay that original path. Run `go test -overlay=PRIVATE/doctor-overlay.json ./cmd/harness-gateway -run '^TestReviewDoctorForeignFragment$' -count=1 -v`. The fixture's running case passing is the defect being demonstrated; a future regression should assert rejection instead.

Do not repeat the resolver probe by binding an unauthenticated gateway to a real network interface. The saved test deliberately used an unassigned documentation address with nonlocal binding disabled. No PID-reuse reproduction was run.
