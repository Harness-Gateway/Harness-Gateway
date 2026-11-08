# EGM finding remediation

Review baseline: `624c4dafb7afd003a000ee886c3a2102744d1786`.
This follow-up tracks the five accepted findings in
[the project review](624c4da-project-review.md). The original reports and saved
probe results remain historical evidence for that baseline.

## Implemented corrections

| Finding | Correction | Regression coverage |
| --- | --- | --- |
| R1 | Consume Copilot events while awaiting the send RPC; cancel and join the send operation on every exit. Close the owned transport before joining a pending socket write. Idle alone cannot hide a send failure. | 256 pre-ack deltas, early idle, failed acknowledgement, cancellation, consumer failure with a pending send, and a session error while a large send write is blocked. |
| R2 | Revalidate the physical installation path after resolving directory symlinks, before installing or editing Bash settings. | A colon-free alias into a colon-containing path fails without invoking Go, creating a binary, or changing `.bashrc`. |
| R3 | Send SIGTERM to the acceptance command group, allowing gateways to cancel their separately grouped CLIs, then force the command group down after at most six seconds. | Real gateways and both gated CLI fixtures under an owner process with Java's group inheritance; an unresponsive group exercises forced cleanup. The original actual-Java probe is also rerun. |
| R4 | Before start/stop/restart/enable/disable/uninstall, query the user manager's LoadState and FragmentPath and require the loaded file to be the managed unit. | All six mutations reject foreign, transient, masked, and missing manager units without mutation; ordinary lifecycle tests preserve user drop-ins. |
| R5 | Generate an OpenAPI 3.0 nullable boolean enum containing false and null, matching the documented handler contract. | Published schema constraints plus handler omission/null/false acceptance and true rejection before backend invocation; regenerate types from the pinned projection. |

The service check is a preflight identity check, not a transaction with systemd;
concurrent external unit replacement remains outside its guarantee. Unloaded or
masked units fail closed. Read-only status/log commands keep their diagnostic
behavior. The acceptance grace interval is bounded and does not extend the
production gateway's shutdown deadline.

## Validation

The parent reran all five original review probes successfully against the fixes.
The pinned OpenAPI validator v0.133.0 also validated the document and explicitly
accepted requests with omitted/null/false `include_obfuscation`, while rejecting
true. The projection was reproduced from the checksum-pinned upstream input;
Go types were regenerated with the pinned generator.

Two fresh-context EGM Codebase Details reviewers inspected the fixes read-only
against the original audit standard. The contract reviewer reported no actionable
R2/R4/R5 issue. The lifecycle reviewer found a conditional blocked-write cleanup
gap in the initial R1 correction. The parent reproduced it: a 900 KiB prompt of
literal `<` expanded into a multi-megabyte SDK frame, and a fixture that stopped
reading that frame but emitted a session error delayed cleanup until the outer
five-second deadline. Closing the owned transport before joining the send fixed
it; the focused regressions passed under the race detector. A narrow read-only
re-audit resolved that finding with no remaining actionable issue. These are
parent-run fixture results and static reviewer conclusions, not real-provider
ordering guarantees. Host/project instruction exclusion from fresh agent contexts
remains unverified.

Final local validation passed: `go fmt ./...`, `go vet ./...`,
`go test -timeout=4m ./...`, `go build ./...`, and
`go test -race -p 1 -timeout=5m ./...`. All five original probes passed. Spring AI
fixture acceptance against the rebuilt gateway passed all 61 tests; browser
acceptance passed all 16 cases using the installed Chrome executable. The
generated OpenAPI document passed validation and checksum-pinned reproduction.

Remote CI must be checked after pushing; the delivery message records its exact
commit and run, including Windows. No authenticated provider inference or public
release is part of this remediation. The original review's unconfirmed hypotheses
and independent live acceptance gaps remain outside the five accepted fixes.
