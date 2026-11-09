# Delivery — round 3 native reviewer reports

## First attempt: `/root/loop_r3_delivery`

Completed all 32 owned text reads plus authorized gzip inspection; hashes matched. Found **delivery-r3-1 — Major: browser acceptance does not clean up on runner termination**, tools/browser/acceptance.mjs:26–36,239–254. Targeted SIGTERM to Node after readiness bypasses JS finally, leaving its idle gateway/runtime and skipping diagnostics. Terminal group SIGINT is a different trigger. Proposed idempotent bounded cleanup for signals, browser, gateway, fixtures, diagnostics and runtime. Static finding only; no runtime reproduction by reviewer.

Isolation clarification: a later inventory read exposed API/provider file completion statuses, including Scalar's explicit exception status. No reviewer_receipts object, agent identities, substantive finding text/conclusions or reports were read. Parent conservatively excluded this attempt as the effective independent audit and dispatched one fresh retry. The candidate was retained for parent verification, rather than discarded as factually false.

## Effective fresh retry: `/root/loop_r3_delivery_retry`

Actual final report: **completed**, all 33 owned file hashes match. All 32 text files fully read; gzip schema/provenance exception inspected. **No concrete findings established in the independent full-area review**; no delivery-r3-retry-N IDs allocated. Truncated common reading recovered separately.

All 32 delivery-owned text paths are recorded exactly in the round-3 execution completion receipt. Mandatory persona/RTK/AGENTS/README/CHANGELOG/round-3 standard/baseline fully read. Inventory records (ownership, format, size, line bounds, hashes and frozen status) read; current-round conclusion fields excluded. Supporting full reads:

```
api/generate.go
api/oapi-codegen.yaml
api/LICENSE.openai
internal/buildinfo/info.go
internal/process/process.go
internal/process/process_unix.go
internal/process/process_windows.go
internal/process/process_other.go
internal/process/wait_linux.go
internal/process/wait_darwin.go
internal/testutil/entrypoint.go
cmd/harness-gateway/installed_test.go
internal/adapters/outbound/codex/installed_test.go
internal/adapters/outbound/copilot/installed_test.go
internal/adapters/outbound/copilot/installed_storage_test.go
```

Gzip exception: timestamp zero and both SHA-256 values match inventory/provenance/projector constant. All selected upstream fields, choices, required lists, nullable forms and ancestors inspected: CreateChatCompletionRequest, CreateModelResponseProperties, ModelResponseProperties, ChatCompletionRequestUserMessage, ChatCompletionStreamOptions, both completion response schemas, message/delta, usage, model/list and error/envelope. Unrelated/discarded schemas and remainder not exhaustively audited. Provenance checked from local records, without network.

Source-level gate coverage found: native Linux/macOS/Windows workflow jobs; separate pinned/latest CLI checks; public tag/module/checksum/artifact verification; distinct Spring/browser/systemd/generation gates and diagnostics; documented authenticated-Windows/reboot/postpublication limits. No claim those jobs actually ran.

## Post-review candidate adjudication

The effective retry was subsequently asked to adjudicate the specific browser interruption candidate. This targeted follow-up is **not** represented as an independent discovery or another round.

Actual conclusion: **Agree — Major, static proof**. No SIGTERM handler routes to finally; targeting Node alone does not signal the gateway. Gateway main.go:205–206,232–250 has no independent parent-exit cleanup. Signal-only interruption can bypass shutdown at browser script:241, writes at252–253 and removal at254. An external supervisor killing the whole tree might prevent an orphan; not every CI cancellation is claimed to leak. Install signal handling before gateway spawn and use one idempotent bounded cleanup for normal completion/signals, gateway wait, browser close, interruption diagnostics, runtime removal and nonzero exit. Regression: target Node after readiness and during a fixture stream; assert bounded runner/gateway/CLI exit, removal and interrupted result.

## Boundaries

Neither attempt nor adjudication ran builds/tests/scripts/browsers/services/installations/generation/network/providers or changed files/Git. No delegation, memory/history retrieval, agent enumeration or other-reviewer inspection; first-attempt metadata exposure disclosed above. No Go runtime-source inference required. Runtime/platform/authenticated/installed/public-release behavior unverified by reviewers. Parent-turn isolation active; host instruction exclusion unverified. Parent fixture verification is recorded separately in the round-3 reproduction receipts.
