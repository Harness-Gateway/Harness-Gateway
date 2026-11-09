# Linux throughput baseline — 2026-11-09

With the same nominal `gpt-6.1-sol` model and `low` reasoning effort, median end-to-end output throughput through the gateway was **22.0 tokens/s for Codex** and **28.9 tokens/s for Copilot**. All 40 measured requests and four excluded warmups succeeded.

## Live results

Each row contains ten sequential requests. TPS is provider-reported output tokens divided by the complete request wall time, including CLI startup, inference and cleanup. First text means the first nonempty assistant-text delta; role-only SSE events do not count.

| Provider | Path | Median TPS | Aggregate TPS | Median first text | p95 first text | Median duration | p95 duration |
|---|---|---:|---:|---:|---:|---:|---:|
| codex | direct | 22.24 | 22.05 | 6.25 s | 14.07 s | 25.29 s | 32.41 s |
| codex | http | 21.99 | 22.21 | 6.36 s | 9.44 s | 24.89 s | 27.21 s |
| copilot | direct | 30.25 | 30.09 | 7.01 s | 8.12 s | 18.05 s | 19.45 s |
| copilot | http | 28.87 | 29.06 | 7.54 s | 8.63 s | 18.82 s | 20.98 s |

Aggregate TPS weights by total measured request time, excluding warmups and gaps. With ten samples, nearest-rank p95 equals the observed maximum; it is not a production tail guarantee.

| Provider | Path | Reported input-token range | Reported output-token range |
|---|---|---:|---:|
| codex | direct | 21452–21452 | 528–587 |
| codex | http | 21452–22281 | 536–600 |
| copilot | direct | 3105–3105 | 526–583 |
| copilot | http | 3105–3105 | 528–605 |

## What this says about gateway overhead

The HTTP median TPS was 1.1% below the direct Codex median and 4.6% below the direct Copilot median. These are observed sample differences, not established transport penalties: outputs vary in length, and remote-provider latency and prompt caching are uncontrolled. Codex HTTP median duration was slightly shorter than direct duration despite its slightly lower median TPS. No statistical equivalence or causal-overhead claim is made.

The separate synthetic benchmark used 32 fixed text chunks (992 text bytes), known usage, and no CLI or upstream calls. Five one-second repetitions per path ran after live inference and validation finished:

| Path | Median of repetition means | Range of repetition means | Median allocated bytes/op | Median allocations/op |
|---|---:|---:|---:|---:|
| direct | 0.0150 ms | 0.0131–0.0194 ms | 1840 | 72 |
| http | 1.2065 ms | 1.1190–1.9632 ms | 125775 | 648 |

The difference between the repetition medians was **1.19 ms** for this synthetic workload. It includes the application, HTTP/SSE server and client, JSON parsing, and measurement bookkeeping. It does not measure installed-service overhead or justify extrapolating to other chunk counts, response sizes, or concurrency.

## Environment and method

- Gateway production source: `v0.1.13`, commit `192e0a0a45f99d07b5e37001ff17169f64220743`, unchanged. Benchmark tooling/docs were uncommitted additions at measurement time.
- Linux/amd64, Intel Core i7-10610U, 8 logical CPUs, Go 1.27.3. Ordinary interactive host; background load and remote service load were not controlled.
- Codex CLI 0.167.0; Copilot bundled runtime **1.0.105**; Copilot Go SDK v1.0.28. See the version-provenance correction below.
- Native model `gpt-6.1-sol`, reasoning effort `low`, concurrency one, timeout three minutes per request.
- One excluded warmup per provider/path, then ten measured requests per group. Provider order was interleaved and direct/HTTP order reversed each round. CLI processes and sessions were fresh on every request.
- Fixed synthetic prompt requested forty numbered, short explanations of Go concepts. Both paths used the same empty temporary working directory and the production adapters' permission/tool policy.
- Direct path used the production Codex app-server / Copilot SDK adapters, bypassing application and HTTP. It did not use interactive CLI mode, `codex exec`, or `copilot -p`.
- HTTP path used the production application and handler on ephemeral numeric loopback TCP with SSE and usage enabled, running inside the benchmark executable. The installed gateway binary/service was not part of this measurement.
- Provider output-token usage may include reasoning. Visible-token decoding TPS cannot be recovered from these counters. Provider-owned system context, token accounting, model routing and prompt caching can differ even for the same nominal model.
- This is one prompt/model/host baseline. It does not establish maximum capacity, concurrent-request behavior, Windows/macOS performance, or latency/throughput for other workloads.
- No local build or test workload ran during the final live measurement. The synthetic result above was collected after final validation.

Live interval: `2026-11-09T14:45:20.69247564Z` to `2026-11-09T15:01:31.291730827Z` (UTC).

## Reproduce and inspect

Use the [runner instructions](../../../tools/benchmark/README.md). The exact command for this run was:

```bash
go build -buildvcs=true -o /tmp/harness-gateway-benchmark-20261109 ./tools/benchmark
/tmp/harness-gateway-benchmark-20261109 -live -samples 10 -warmups 1 \
  -model gpt-6.1-sol -reasoning-effort low \
  -output-dir docs/performance/2026-11-09/live
```

Choose a new output directory for another run. Authenticated inference consumes account allowance.

- [Raw request samples](live/samples.jsonl), [summary](live/summary.json), [metadata](live/metadata.json), and [completion receipt](live/result.json).
- [Independent verification](independent-verification.json) recomputed every statistic and token/time relationship from all 44 raw requests and checked unchanged production sources.
- [Synthetic raw output](synthetic.txt) and [machine-readable synthetic summary](synthetic-summary.json).
- [Measured source hashes](source-sha256.json), [build provenance](build.txt), [host details](host.txt), [gateway logs](live/gateway.log), and [runner output](run.log).
- [Validation receipt](validation.json): formatting, vet, all 701 tests across 17 packages, and both executable builds passed. Regressions cover actual production SSE parsing, ignored role-only chunks, stream failures/truncation, missing usage, weighted aggregation/percentiles, bundled-version policy, bounded output, and subprocess cancellation.

## Copilot version-provenance correction

The original runner queried plain `copilot --version`, which reported an automatically selected cached version, 1.0.107. The production adapter already passes `--no-auto-update` for both direct and HTTP inference and actually used the bundled **1.0.105** runtime. The original metadata is retained unchanged; [runtime provenance](runtime-provenance.json) and the [explicit bundled-runtime query](copilot-runtime-version.txt) record the correction.

The reusable runner was subsequently corrected to query the bundled runtime, bound version output, and own version-probe subprocess cleanup. This changed preflight provenance only; the timing, request, CLI adapter and aggregation code used for the live run is unchanged. The [follow-up patch](runner-provenance-followup.patch) can be reversed against the accompanying runner to reconstruct the measured runner source; its hash was independently verified. Final runner source hashes and regression validation are in the validation receipt.
