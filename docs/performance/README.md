# Performance measurements

Use [the benchmark runner](../../tools/benchmark/README.md) to reproduce live
Codex/Copilot throughput measurements and synthetic local transport measurements.

Results are specific to the recorded host, CLI versions, account/provider state,
native model, prompt, reasoning effort, and concurrency. They are observations,
not guaranteed service levels or maximum-capacity claims.

- [2026-11-09 Linux baseline](2026-11-09/README.md): source implementation at
  v0.1.13, with direct adapter and loopback HTTP/SSE measurements.
