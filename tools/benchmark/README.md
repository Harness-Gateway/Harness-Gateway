# Live throughput benchmark

Run from the repository root after logging in to both CLIs:

```bash
go run ./tools/benchmark -live \
  -model gpt-6.1-sol -reasoning-effort low \
  -samples 10 -warmups 1 \
  -output-dir /tmp/harness-gateway-benchmark-results
```

The output directory must not exist. `-live` explicitly enables authenticated
inference and consumes account allowance. Choose a native model supported by both
accounts. The fixed synthetic prompt requests forty short numbered Go explanations;
no repository data is submitted. CLIs use the same temporary empty working directory
for both routes, with the production adapters' read-only/disabled-tool policies.
Existing authentication is used; the installed gateway service is not changed.

Each provider gets one excluded warmup and ten measured requests on each path:

- `direct`: call the production provider adapter, including its ephemeral CLI
  process, session setup, protocol, usage collection and cleanup.
- `http`: the production application and OpenAI HTTP handler, served on an
  ephemeral numeric loopback TCP port, consumed by an SSE client with usage enabled.

This baseline bypasses application/HTTP handling while preserving CLI protocol
and session policy. It does not invoke interactive CLI mode, `codex exec`, or
`copilot -p`. HTTP runs in the benchmark process; this is a source implementation
measurement, rather than an installed executable/service or remote-network test.

All requests run sequentially at concurrency one. Provider order is interleaved;
direct/HTTP order is reversed every round to reduce ordering bias. Every completion
starts a fresh CLI process, even after warmups. Warmups can warm OS/provider caches,
but there is no reused CLI session. Provider prompt caching is not controlled.
Built-in CLI retries may occur; the runner does not retry failed samples.

## Metrics and evidence

`samples.jsonl` retains every request, including warmups, with timestamps, total
wall duration, first/last nonempty assistant-text arrival, provider usage, finish
reason, text bytes/chunk count and a text SHA256. Prompt and CLI/build provenance
are in `metadata.json`. `gateway.log` contains request diagnostics.
`summary.json` is updated after every completed sample; `result.json` records
completion/failure. Failed requests stop the run and never contribute TPS.

End-to-end TPS is **provider-reported output tokens / full request seconds**.
Duration includes CLI startup, inference, and cleanup through direct completion
return or receipt of SSE `[DONE]`. First-text timing ignores role-only SSE chunks.
Missing text, missing positive token usage, stream errors, non-success finishes,
and truncated streams fail the measurement. No token counts are inferred from
bytes, words, or SSE chunk counts.

Provider output usage may include reasoning tokens. The gateway's usage structure
does not distinguish those from visible text, so this is not pure visible-token
decoding TPS. Provider-owned system prompts/context and accounting can differ even
for the same native model name. Compare each provider's two paths before drawing
cross-provider conclusions.

Summaries report per-request median/min/max and nearest-rank p95. Aggregate TPS is
total output tokens / total successful measured request time, excluding warmups
and between-request gaps. With ten samples, nearest-rank p95 is the maximum;
it is descriptive and does not establish a reliable production tail bound.
Remote service variability prevents a small live direct/HTTP difference from
establishing gateway overhead or statistical equivalence.

## Isolating local transport overhead

The synthetic benchmark replaces the CLI with 32 fixed text chunks and known
usage. It exercises the production application, HTTP handler, SSE client, and
measurement bookkeeping, isolating local work from remote inference variance:

```bash
go test ./tools/benchmark -run '^$' -bench '^BenchmarkTransport$' \
  -benchmem -count 5 -benchtime 1s
```

This reports mean nanoseconds per operation and allocations for each repetition,
not live model TPS or per-request p95. It is an in-process loopback measurement;
installed service scheduling, disk logging, larger responses, concurrent traffic,
and different client/network stacks require separate measurements.
