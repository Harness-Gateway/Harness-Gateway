# Diagnostics and request observability contract

Status: retrospective contract reviewed in the bounded EGM pilot; see the
[review report](../reviews/6476397-egm-review.md) for findings and verification.
Review baseline: `6476397885b54ee9345b766274ae196cc3d90b90`.
Scope: the diagnostic command and request observation added in that commit.
This document is a review target, not evidence that the implementation meets it.

## Purpose and boundaries

The gateway exposes text Chat Completions through local Codex/Copilot processes.
This change helps a local user diagnose a running gateway and distinguish request
outcomes. It adds neither another inference API nor a public telemetry endpoint.

The normative requirements below express intended behavior. The implementation
map describes the baseline separately. Reviewers should report discrepancies;
they must not treat existing code or tests as the source of desired behavior.

## Required behavior

### D1: Explicit diagnostic scope

`doctor` reports the inspecting executable's version/build/path, local
configuration, enabled CLI versions, gateway liveness, and model discovery.
Foreground flags describe the local checks and listener to contact. The caller
must supply settings matching the foreground server. Passing those checks does
not establish that the server uses the same configuration or executable.

`doctor --service` requires a managed Linux user service. It inspects only an
active/running invocation, reads that invocation's startup record, and uses its
recorded listener, CLI paths, and working directory. Missing current metadata or
an inactive service fails without probing a historical listener. A pinned CLI
path differing from shell PATH is informational, not itself a failure.

Comparing release versions must not claim byte-for-byte executable identity.
The inspecting and recorded running build identities must remain visible.
The journal is a startup snapshot; this change does not introduce an atomic
service-identity protocol spanning inspection and subsequent HTTP requests.

### D2: Inference is opt-in

Health and discovery are separate checks. Health reports liveness only; discovery
may start CLI processes and contact their services, and its result may be cached.
Neither proves inference authentication or model entitlement.

Only `--probe MODEL` performs inference, with one non-streaming request using the
specified model and a fixed short prompt. There are no automatic retries. A probe
passes only when the response object is `chat.completion` and the first choice
contains an `assistant` message with nonempty text. This is a basic response
check, not full schema validation. A successful default diagnostic must not
imply that inference was exercised.

### D3: Bounds, local access, and cleanup

Diagnostics share a configurable total deadline, defaulting to 30 seconds.
Each CLI version query has at most 20 seconds and retains the existing bounded
output capture and owned-process cleanup. Context cancellation must stop owned
version-query processes and HTTP operations.

HTTP checks accept plain HTTP loopback addresses only, bypass environment
proxies, and do not follow redirects. Each JSON response is capped at 1 MiB.
Failures must yield a nonzero command exit status and a readable check report.
Independent useful checks should still run where their prerequisites permit.
Diagnostics must not install, restart, enable, or reconfigure the service.

### O1: Correlation before admission

Requests to `/v1/chat/completions` and `/v1/models` receive a server-generated
`X-Request-ID` before origin, method, and body checks. Client-supplied IDs are not
trusted. Completion IDs and associated log records use that same generated ID.
This requirement includes rejected requests and cached model responses.
Health, documentation, and unrelated routes are outside this observation scope.

### O2: Final outcome records

Each observed request has one final summary with request ID, method, path, actual
started HTTP status, outcome, and total duration. No started response is recorded
as status zero. The supported outcomes are `success`, `rejected`, `timeout`,
`canceled`, `backend_error`, `shutdown`, and `write_error`.

The record must preserve an established failure classification, including during
context-cancellation races. A transport failure takes precedence when delivery
of an already selected error response also fails. Final context inspection is a
fallback only while no failure has been recorded. Failure codes are included
when available. An SSE request may have HTTP status 200 and an unsuccessful
outcome. Once an operation failure is known, the handler must not start success
frames or `[DONE]`. A later terminal-frame write/flush failure cannot retract
bytes already written; it must record `write_error` and stop subsequent writes.

The observation wrapper must preserve socket write deadlines and SSE flushing.
Buffered write failures, SSE write/flush failures, and failures while sending
terminal frames must be visible. Existing backend cleanup and admission behavior
must remain intact. Handler panics must not be swallowed by the observer.

### O3: Timing and content boundaries

Total duration starts before request validation. A completion receiving nonempty
backend text records time to first text from that same start. The initial
role-only SSE frame and empty backend emissions do not count. Zero is a valid
measured latency and must remain distinguishable from no text. This measures
backend text availability, not necessarily successful delivery to the client.

New summary records contain no prompt, response text, or request body. Existing
backend diagnostic logs are separate and are not given a new privacy guarantee
by this change. JSON timing fields use duration values in nanoseconds.

## Implementation map at the baseline

| Component | Source and responsibility |
| --- | --- |
| Command dispatch | [main.go](../../cmd/harness-gateway/main.go): routes `doctor` and handles its process exit |
| Diagnostic checks | [doctor.go](../../cmd/harness-gateway/doctor.go): configuration, service, CLI version, health, discovery, and optional probe |
| Service inspection | [service_status.go](../../cmd/harness-gateway/service_status.go): shares systemd properties and invocation-scoped journal lookup with the dashboard |
| Version queries | [versions.go](../../cmd/harness-gateway/versions.go): bounded output and owned CLI process cleanup |
| HTTP observation | [observe.go](../../internal/adapters/inbound/openai/observe.go): IDs, response writer, and final summaries |
| HTTP operations | [handler.go](../../internal/adapters/inbound/openai/handler.go): validation, discovery/completion outcomes, SSE, and write deadlines |
| Local protection | [security.go](../../internal/adapters/inbound/openai/security.go): loopback Host and same-origin protection |
| Backend lifecycle | [gateway.go](../../internal/application/gateway.go), [models.go](../../internal/application/models.go), and [process](../../internal/process): admission, shared discovery, cancellation, and process ownership |

## Acceptance evidence and remaining work

Before independent review, the author ran formatting, vet, build, 546 Go tests,
the race detector, 61 Spring AI fixture acceptance tests, and 16 browser cases.
The browser cases used installed Chrome because the pinned Playwright browser
download was absent. These are author-operated results from this checkout.

The built diagnostic command also checked the installed v0.1.10 service and
discovered 25 model IDs/aliases without inference. That checks command dispatch
and real discovery, but does not deploy the new observation middleware into the
installed service. Probe request behavior has mock HTTP coverage; authenticated
inference against a release candidate remains a separate acceptance gate.

CI and release status must be queried live; this document does not freeze their
state. Neither a clean review nor tests establish hardware boot/pre-login
acceptance, authenticated Windows inference, or public release/install success.

## Focus of the bounded pilot

Review these boundaries rather than requesting new product scope:

- Cancellation, timeout, shutdown, and write-error precedence in final outcomes.
- Writer-interface behavior, slow readers, and terminal SSE frame failures.
- Whether `doctor` reports failures and evidence strength accurately.
- Current-invocation versus historical settings and service restart races.
- Differences between documented behavior, source behavior, and test evidence.

Each finding needs a requirement ID, file/line evidence, concrete trigger and
effect, and a proposed correction or acceptance check. Label static reasoning
separately from reproduced runtime evidence. Cosmetic nits do not block release.
