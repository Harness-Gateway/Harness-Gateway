# Project-wide EGM review

Date: 2026-11-08, Europe/Rome.
Baseline: `624c4dafb7afd003a000ee886c3a2102744d1786` (`dev`).
Remediation: [fixes and validation](624c4da-fix-validation.md).
Scope: all 128 tracked files, with the vendor/dependency limits below.
Method: installed EGM 0.1.1 personas, fresh parent-turn contexts, independent
domain reviews, an independent architecture pass, and parent-run fixture probes.
This is a source-oriented adaptation, not the stock design-approval workflow.

## Result

The review found three reproduced implementation/tooling defects, a conditional
service-ownership gap demonstrated with a mock, and one reproduced schema/runtime
disagreement. Further robustness and acceptance gaps need narrower validation.
No P0/P1 defect or remotely exploitable vulnerability was established.

The baseline's [Validate run](https://github.com/Harness-Gateway/Harness-Gateway/actions/runs/42518081833)
completed successfully, including all eleven Linux/macOS/Windows jobs. Passing
CI did not cover the newly reproduced scenarios. The failing review probes below
are added through Go overlays; they are not failures of the unchanged baseline
test suite. No production source or tracked test was edited by this audit.

| ID | Priority | Finding | Evidence and reachability |
| --- | --- | --- | --- |
| R1 | P2 | Copilot event backpressure can block the send acknowledgement | Reproduced with 256 one-byte pre-reply events; actual public CLI occurrence unverified |
| R2 | P2 | Installer canonicalization bypasses PATH-separator validation | Reproduced with a colon-free alias to a colon-containing directory |
| R3 | P2 | Interrupted acceptance can leave a gated CLI fixture alive | Reproduced using actual Java fixture gateway and the runner's cancellation helper |
| R4 | P2 | Lifecycle ownership checks do not bind to the manager-loaded unit | Dispatch reproduced with fake systemctl; mismatched live-manager condition not exercised |
| R5 | P3 | Published schema rejects a documented accepted null value | Actual handler accepts null; published boolean/enum constraints exclude it |

## R1: Copilot can stall before consuming a small answer

Source: COP-01. Disposition: ACCEPT, conditional protocol-ordering defect.
Location: [copilot.go](../../internal/adapters/outbound/copilot/copilot.go#L105),
lines 105-120; pinned SDK v1.0.28 `session.go` event buffering/dispatch.

The gateway subscribes a callback that queues events in 64 slots, then waits
synchronously for `session.Send` before starting the consumer loop. The SDK has
another 128-event queue and synchronously queues notifications from its protocol
reader. Enough notifications before the `session.send` result fill both buffers
and prevent the reader from reaching that result.

The local fixture sent 256 one-byte assistant deltas, then the send result and
idle. The one-second request deadline expired with **zero text bytes emitted**:
`send copilot request: failed to send message: context deadline exceeded`.
The ordinary fixture replies before emitting events and misses this ordering.

This proves the gateway failure for that valid notification ordering. It does
not prove the pinned public CLI currently emits such a burst before its reply.
Cancellation still bounds the stall and cleans the owned transport.

Recommended correction: consume events while a coordinated send operation is
pending; cancel and join that operation on all exits. Add the pre-reply burst
fixture as a regression and validate real CLI ordering independently.

Reproduce: `python3 docs/reviews/624c4da-repros/run.py copilot-burst`.
Saved evidence: [burst-result.txt](624c4da-repros/burst-result.txt).

## R2: Installer can persist an unusable PATH

Source: DIST-01. Disposition: ACCEPT, reproduced defect.
Location: [setup.sh](../../setup.sh#L39), lines 39-42 and 63-67.

The script rejects a colon in the supplied directory, then canonicalizes it
with `pwd -P`. A safe-looking symlink can resolve to a physical directory that
contains a colon. The script successfully installs and appends that physical
directory to the temporary Bash profile. Shell quoting does not stop PATH from
interpreting the colon as a separator.

In the fixture, installation succeeded but a fresh Bash lookup still selected
the pre-existing executable instead of the new installation. The probe used a
fake Go installer and a temporary profile; the real Bash profile was not changed.

Recommended correction: validate the canonical path before invoking Go or
changing shell configuration, or retain a deliberately supported logical alias.
Test that installation either rejects the path without side effects or produces
a working command lookup.

Reproduce: `python3 docs/reviews/624c4da-repros/run.py installer-path`.
Saved evidence: [installer-result.txt](624c4da-repros/installer-result.txt).

## R3: Acceptance interruption can strand a gated fixture

Source: ACC-01. Disposition: ACCEPT, reproduced tooling defect.
Locations: [process_unix.go](../../tools/acceptance/process_unix.go#L13), lines
13-22; [FixtureGateway.java](../../examples/spring-ai/src/test/java/io/harnessgateway/FixtureGateway.java#L60),
lines 60-76; fixture catalog-gate loops in Codex/Copilot tests.

On cancellation, the acceptance runner SIGKILLs Maven's process group. Java's
fixture gateways inherit that group and cannot run their cleanup. Their CLI
fixtures use separate process groups. During `catalog-gate`, a fixture polls a
release marker without reading stdin, so closed pipes do not retire it.

The probe ran the actual Java `FixtureGateway` and Go Codex fixture, reached the
catalog gate, then canceled the Java process using `configureCommand` and its
real `killCommand` helper. The gateway died, but the gated CLI remained alive.
The probe issued a group kill afterward. The parent separately checked the
recorded fixture executables were absent after both runs; see the
[cleanup receipt](624c4da-repros/cleanup-receipt.json). This reproduces the cancellation topology, not an end-to-end Maven
interruption or a real-provider/production process leak.

Recommended correction: coordinate graceful cleanup of Java-owned gateways and
their recorded CLI groups before forcefully terminating Maven. Add an interrupt
acceptance case that asserts all recorded PIDs exit, including a closed gate.

Reproduce on Linux after ordinary Java fixture acceptance has prepared classes:
`python3 docs/reviews/624c4da-repros/run.py acceptance-interrupt`.
Saved evidence: [interrupt-result.txt](624c4da-repros/interrupt-result.txt).

## R4: A managed file is not proof of the unit being controlled

Source: OPS-01. Disposition: ACCEPT as conditional ownership/hardening gap.
Locations: [service.go](../../cmd/harness-gateway/service.go#L132), lines 132-166
and 343-352. `inspectService` already obtains `FragmentPath` for status reporting.

Lifecycle operations verify the managed marker in the caller's configuration
directory, then address systemd's bare service name. They never establish that
the manager loaded that same file. A manager/caller `XDG_CONFIG_HOME` mismatch or
a higher-priority same-name unit can make those identities differ.

The mock configured a foreign `FragmentPath`; `stop` still dispatched without
querying it. This proves the missing guard, not a live foreign-unit shutdown.
The README already tells users to match custom XDG settings to the manager, so
the mismatch is outside the documented normal setup. The recommendation is a
defensive refusal, not a claim of a privilege escalation or default-install bug.

Recommended correction: bind mutation authorization to the actual loaded
fragment identity, accounting for unloaded units, transient/runtime precedence,
and user drop-ins. Confirm real manager resolution in a disposable account.

Reproduce the dispatch gap without accessing the real manager:
`python3 docs/reviews/624c4da-repros/run.py service-identity`.
Saved evidence: [service-result.txt](624c4da-repros/service-result.txt).

## R5: Schema and runtime disagree about null obfuscation

Source: HTTP-01. Disposition: ACCEPT, contract discrepancy.
Locations: [openapi.json](../../api/openapi.json#L380), lines 380-385;
[contract.go](../../tools/project-openapi/contract.go#L23), lines 23-25;
README compatibility table and handler stream-option validation.

The README permits omitted, null, or false `include_obfuscation`, and the handler
accepts null as a nil pointer. The published property is a nonnullable boolean
with enum `[false]`, which excludes null. The fixture received HTTP 200 for null
and inspected those contradictory published constraints. No external schema
validator was run; the type/enum exclusion follows directly from the document.

Recommended correction: express the intended nullable false-only value in the
projection generator for its OpenAPI edition, regenerate specification/types,
and test schema/handler agreement for null, false, and true. True must remain
rejected before starting a harness.

Reproduce: `python3 docs/reviews/624c4da-repros/run.py http-schema`.
Saved evidence: [schema-result.txt](624c4da-repros/schema-result.txt).

## Robustness and validation gaps

### V1: No aggregate byte budget for queued Copilot events

Source: COP-02. Disposition: NEEDS VALIDATION, P2 robustness gap.
`frames.go:14-16,61-71` permits individual frames up to 97 MiB; the gateway and
SDK retain 64 and 128 events respectively. The 16 MiB assistant-text check runs
when `emit` consumes a delta, not before those queues retain it. A stalled SSE
write plus abnormal large accepted frames can therefore retain far more memory
than the emitted-text limit suggests. Ignored event payloads can also queue.

This is static amplification evidence. No peak RSS, OOM, normal-provider trigger,
or untrusted-network exploit was demonstrated. Frame caps, finite queues, global
concurrency, and deadlines remain real defenses. Add a modest, controlled byte-
budget/blocked-consumer probe before selecting an aggregate allocation limit.

### V2: Unix foreground cleanup is a process-group guarantee

Sources: OPS-03 and the Codex/Copilot reviewers. Disposition: ACCEPT validation
boundary, P2 if complete descendant containment is a release requirement.
`internal/process/process_unix.go:15-22` owns a process group. A descendant that
creates another group/session escapes that group's kill. Ordinary daemonization
can do this; no inspected evidence shows a pinned supported CLI doing so.

Do not describe this as a demonstrated provider leak. Test detached descendants
and define the foreground guarantee. Systemd cgroup cleanup and Windows Job
Objects are distinct stronger mechanisms; their acceptance must remain separate.

### V3: Upstream-to-projection reproducibility is not an end-to-end CI gate

Source: distribution reviewer. Disposition: ACCEPT validation gap, no present
schema drift established beyond R5. `tools/project-openapi/main_test.go:12-25`
derives schemas from the checked-in embedded artifact; Validate regenerates Go
types from that artifact. Neither comparison independently rebuilds the complete
pinned upstream-to-projected specification. Add a pinned upstream input fixture
or checksum-verified fetch/reprojection comparison when maintaining projection
selection/normalization. No upstream download/regeneration was performed here.

### V4: Existing explicit acceptance boundaries remain open

Authenticated inference of a release candidate, authenticated Windows inference,
boot/pre-login credentials, and fresh public-release/install/deployment evidence
are separate gates. They are documented boundaries, not newly discovered missing
API implementations. The review did not run providers or mutate the installed
service. The current `main` branch still points at the v0.1.10 release commit;
`dev` contains the reviewed unreleased changes as of the recorded remote check.

## Candidates not promoted to confirmed defects

| Raw candidate | Disposition and evidence still needed |
| --- | --- |
| COP-03: SDK ForceStop concurrent with client calls | NEEDS VALIDATION. Source access patterns deserve scrutiny, but 100 local catalog-cancellation iterations under `-race` passed. No race or panic was reproduced; cancellation checks invalidate the initial simple nil-panic argument. See [race-result.txt](624c4da-repros/race-result.txt). |
| CDEX-1: Foreign thread/turn notifications accepted | NEEDS VALIDATION. The adapter does not correlate notification IDs. It would misattribute injected foreign events, but whether the public CLI sends them on this connection without subscription is unverified. Require version-matched protocol/broadcast evidence before calling it a supported-workflow bug. |
| OPS-02: localhost resolves to nonloopback | NEEDS VALIDATION, low-priority boundary hardening. Literal hostname acceptance does not validate the resolved/bound address. Exposure needs abnormal host resolution; none was demonstrated. Pinning localhost to a literal loopback would remove that assumption. |
| Failed daemon-reload restores disk but not verified manager state | NEEDS VALIDATION. File restoration is tested and documented accurately. A canceled/partially accepted manager reload requires a disposable-manager probe; no runtime rollback failure was shown. |

## Coverage, independence, and limits

Six domain reviewers completed, each with `fork_turns: "none"`: `full_http`,
`full_codex`, `full_copilot`, `full_operations`, `full_distribution`, and
`full_acceptance`. A separate `full_architecture` zoom-out reviewer read the
whole architecture without seeing their candidate findings and found no
additional actionable architecture issue. All returned actual reports and
reported reading their persona, the baseline/standard, assigned files, and
necessary cross-calls. Missing reviewers were not counted as clean.

[coverage.json](624c4da-coverage.json) inventories all 128 baseline files and
review ownership. Scalar's upstream JavaScript internals were not read byte by
byte; its SHA-256, license, embedding, CSP, and browser controls were inspected.
Generated API documents/types and their generators were inspected. This is not a
complete dependency audit; the pinned SDK was inspected where gateway behavior
depended on it. Root documentation/history inspection supplements fresh source
reviews; it is not itself a fresh independent review of historical evidence.

Fresh parent-turn contexts do not prove exclusion of shared host/project
instructions. No reviewer retrieved parent chat or memory to fill missing design
context. Reviewers did not execute tests; reproductions above were parent-run
and use local fixtures only. The successful CI result is evidence for its exact
commit, not proof of all invariants or acceptance on every host configuration.

The [independent final adjudication](624c4da-adjudication.md) completed and
sustained R1-R5 and the qualifications on the remaining candidates. Its one
evidence-wording nit was resolved by recording the separate cleanup receipt.
No review can establish that all hidden issues have been found.

## Suggested implementation order

1. Address R1's send/event progress and R3's interruption cleanup with deterministic
   regressions; validate the relevant real CLI/Maven timing separately.
2. Fix R2's canonical PATH validation and add R4's manager identity refusal tests.
3. Regenerate the contract for R5 and add the upstream-projection gate from V3.
4. Measure V1 with a controlled allocation budget, define V2's process guarantee,
   and resolve remaining protocol/race candidates with focused evidence.

The audit itself changed no production code or installed service and performed
no Git publication. Subsequent authorized remediation is tracked in the
[fix-validation report](624c4da-fix-validation.md).
