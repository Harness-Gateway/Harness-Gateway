# Remaining EGM gap closure

Starting point: `b6179f240cd10101224e2d5311c5f72fee24c40d` on `dev`.
Scope: V1-V3 in [the project review](624c4da-project-review.md), regression tests,
changelog, and automated validation. This does not qualify authenticated inference
or publish a new release.

## V1: Copilot event backlog

Each Copilot bridge reserves serialized frame bytes before reading the body and
before SDK allocation. The per-operation allowance is 128 MiB. `session.event`
notifications remain charged until the gateway takes them for processing, across
both SDK and gateway queues, including ignored subagent events. Event IDs retain
FIFO reservations when repeated. Non-event RPC frames release their reservations
after forwarding. A frame that cannot fit fails admission before body allocation;
transport failure stops that owned CLI and propagates a backend error. One event
being processed may remain live in addition to the backlog. Frames retain their
97 MiB maximum, including the JSON-escaping allowance for 16 MiB text.

The application subscribes after session creation, preserving SDK startup
progress. Unobserved startup notifications stay conservatively charged until the
operation ends rather than bypassing admission. Cancellation drops callbacks
without waiting for the application queue. The existing coordinated send/event
loop and transport close before joining a pending Send remain intact.

This is a wire-byte backlog policy, not an exact heap/RSS limit. Decoded objects,
read buffers, a current event, concurrent requests, SDK broadcast work and provider
processes add memory. No RSS ceiling or absence of OOM under every malformed
provider payload is claimed. Notifications that the SDK does not route to the
owned session remain charged until the operation ends, so an unmatched-event flood
also fails closed rather than bypassing the allowance.

Regressions cover pre-body admission, duplicate IDs, ordinary RPC release, truncated
and invalid JSON, recycling over 256 events, maximum-frame policy, and a blocked
real SDK consumer. The latter uses a modest 32 KiB allowance: an initial parent
probe observed 25,434 reserved wire bytes and 205,512 Go bytes allocated over the
measured Send/failure window. These small-fixture allocation observations are not
production RSS measurements. The guarded transport also has a fuzz target.

## V2: Unix cleanup contract

Foreground cleanup is explicitly a process-group guarantee. A CLI helper that
creates a different process group or session escapes per-request cleanup; it must
not do so in the supported foreground workflow. This change does not claim a
portable Unix equivalent of Windows Job Objects and does not scan/kill arbitrary
same-user PIDs. Process API documentation and README state this boundary.

Unix regressions run actual descendants in inherited groups, new groups and new
sessions. They verify owned-group termination, preservation of the escaped
boundary, idempotence, and explicit cleanup of every detached fixture by the test.

The real Linux systemd lifecycle test now creates detached helpers in the actual
service cgroup and verifies new-session IDs and cgroup identity before each stop,
restart and main-process crash. It requires their exit after the existing mixed
kill policy's bounded cgroup cleanup. This protects service teardown, not cleanup
of a detached helper at the end of an individual request while the service stays
running. Local tests cannot substitute for this disposable-manager CI gate.
The behavior of the existing mixed kill policy is described in the
[systemd kill documentation](https://www.freedesktop.org/software/systemd/man/latest/systemd.kill.html).

## V3: Complete OpenAPI projection CI

The exact official upstream JSON is checked in as a deterministic compressed test
input with source commit, uncompressed checksum, and MIT license attribution.
A regression decompresses it, invokes the real checksum-verifying projector, and
compares the entire result to the embedded published schema. It also verifies
that a modified upstream input is rejected. The CI matrix explicitly runs this
check on Linux/macOS/Windows before Go type regeneration; it requires no upstream
network fetch. The existing schema/example validator and generated-type drift
check remain separate gates.

## Validation and review

Local validation passed: formatting, vet, full Go tests, build, and the full race
suite. After the final RPC-routing adjustment, the complete Copilot package was
rechecked normally and under the race detector. Four transport/authentication
fuzz targets passed ten-second runs; the two affected framing/budget targets were
rerun after that adjustment. Pinned upstream projection, schema validation and Go
type regeneration checks passed. Spring AI fixture acceptance passed 61 tests
against the rebuilt gateway; browser acceptance passed 16 cases with installed
Chrome. The original 256 pre-send-acknowledgement event probe also passed.

Two fresh-context EGM Codebase Details reviewers completed read-only source
reviews. One found a creation-time queue cycle in an intermediate early callback
subscription. The parent reproduced its five-second timeout, restored subscription
after creation, and added 256-notification startup success/error/cancellation
regressions, which passed under the race detector. The other requested an explicit
session-ID assertion in the systemd regression; both SID and PGID are now checked.
Narrow re-audits resolved both findings with no remaining issue in their scopes.
Host/project instruction exclusion from fresh agent contexts remains unverified;
reviewers did not execute tests or providers.

The final delivery records the exact pushed commit's CI run, including the real
disposable-systemd gate and Windows. Historical reports retain their original
baseline claims; this document records the changed contracts and evidence limits.
