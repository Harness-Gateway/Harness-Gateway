# Project review baseline

Review target: `624c4dafb7afd003a000ee886c3a2102744d1786` on `dev`.
Purpose: source-oriented EGM audit of the entire tracked project, including
runtime adapters, operations, tooling, acceptance tests, and documentation.
This baseline is a map of claims to challenge, not proof of correctness.

## Product purpose

Harness-Gateway is a loopback-only, single-user Go HTTP gateway that starts
request-owned Codex and Copilot CLI processes using the OS user's login.
It supports text Chat Completions, streaming, and native model discovery for
clients including Spring AI. It does not attach to interactive terminal sessions.

Declared exclusions include Responses, embeddings, multimedia, client-defined
tools, sampling parameters, persistent cross-request sessions, multitenancy,
public network exposure, and automatic retries. These are not implementation
defects by themselves; discrepancies in their rejection/documentation are in scope.

## Public and internal contracts to challenge

- API: generated wire types project the supported OpenAI contract; invalid or
  unsupported input receives explicit errors. Stream admission keeps 404/429;
  subsequent failures use SSE errors without starting new success frames.
- Limits: request bodies 1 MiB, decoded assistant text 16 MiB, bounded operation
  duration and concurrency, ten-second response-write budgets. Limits must cover
  memory/resource retention and cleanup, not just the final returned status.
- Routing: aliases and explicit native IDs select enabled backends. Native
  catalog discovery is shared/cached for 30 seconds, has a bounded refresh, and
  shares CLI capacity with completions. Caller cancellation is independent.
- Lifecycle: contexts reach backend operations; request-owned process trees and
  SDK sessions retire on success, failure, cancellation, and shutdown. Child-held
  pipes and frame reads must not indefinitely conceal a dead backend.
- Security: unauthenticated HTTP is restricted to loopback and protects browser
  origins/Host. Codex uses a read-only sandbox and never-approve policy. Copilot
  rejects permissions and uses an empty tool allowlist and private request state.
  The private Copilot bridge authenticates connections and bounds frame allocation.
- Conversation adaptation: initial instructions and role-labelled history are
  contextual input to a harness, not exact model-API semantics or an isolation
  boundary against malicious model content. Reasoning/model settings must not
  be silently ignored.
- Operations: executable/path/version reporting must describe its actual evidence.
  Systemd changes are opt-in, protect foreign units, preserve settings/drop-ins,
  and clean their runtime workspace. `doctor` avoids inference without `--probe`.
- Distribution: versioned Go/Bash installs, build identity, API generation, pinned
  assets, CI matrices, canaries, and public-release verification must be reproducible
  and must test the actual relevant artifact. No generated source is hand-edited.
- Validation: mocks/fixtures, installed-package handshakes, authenticated inference,
  browser acceptance, systemd tests, and platform/boot acceptance have different
  evidence boundaries. Green tests do not establish untested guarantees.

## Source map and review ownership

| Review area | Primary files |
| --- | --- |
| HTTP API, browser security, schema | `internal/adapters/inbound/openai/`, `internal/application/`, `internal/domain/`, `api/` |
| Codex protocol and prompt adaptation | `internal/adapters/outbound/codex/`, `internal/adapters/outbound/prompt/` |
| Copilot SDK, bridge, frames, storage | `internal/adapters/outbound/copilot/` and the pinned SDK interfaces it invokes |
| CLI, systemd, process ownership | `cmd/harness-gateway/`, `internal/process/`, `internal/buildinfo/`, `internal/testutil/`, `tools/systemd/` |
| Build, installs, releases, generation | `.github/workflows/`, `setup.sh`, `go.mod`, `go.sum`, `tools/{setup,releaseverify,project-openapi}/` |
| Java/client/browser acceptance | `examples/spring-ai/`, `tools/acceptance/`, `tools/browser/`, root and asset documentation |

The full tracked-file inventory is captured in `624c4da-coverage.json` beside
this document. Generated types/specifications are checked through their generators
and validators. Vendored Scalar code is assessed through version/hash/license,
embedding, CSP, and browser-boundary checks; no claim of a complete upstream
JavaScript internals audit is made. Dependencies are inspected where required to
verify gateway calls, not subjected to a complete dependency source audit.

## Audit method and result boundaries

Use installed EGM 0.1.1 personas in fresh parent-turn contexts, explicit source
reading lists, and the [project audit standard](624c4da-review-standard.md).
This adapts EGM's independent critiques and evidence-based reconciliation to
an existing implementation rather than running its design-document approval UX.

Reviewers report findings only. The parent verifies reachability, deduplicates
claims, and uses offline/local-fixture reproductions where useful. Production
sources, installed services, credentials, and Git branches remain unchanged by
the audit. Reproduction code and reports may be saved as review artifacts.

The requested breadth does not make the review exhaustive. Final reporting must
identify uncovered/partially covered areas and distinguish reproduced defects,
static findings, validation gaps, intentional limitations, and contested claims.
