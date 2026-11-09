# Entire-repository EGM round 3 baseline

Commit: `73da315fc9d384595c2bf229a37abf3963fcfb82`; review the current uncommitted working tree, not the clean tag.
Snapshot SHA-256: `be5f7f63d85065876f8691c6c261e7bd06513f64d3fc99cf9ea5829c6c7a2ceb`.

Target: all 236 repository files present before round-3 scaffolding, full-file scope. Current line bounds and per-file hashes: [73da315-loop-r3-coverage.json](73da315-loop-r3-coverage.json).
Standard: [73da315-loop-r3-standard.md](73da315-loop-r3-standard.md).

| Area | Owned files |
| --- | ---: |
| api-http | 31 |
| cli-service | 23 |
| delivery | 33 |
| docs-examples | 106 |
| process-lifecycle | 16 |
| providers | 27 |

Mandatory common reading: this baseline, round-3 standard and manifest, repository README.md and AGENTS.md. Read every owned file fully, including tests. Follow cross-area calls as necessary. Six fresh-context reviewers run strictly read-only. No previous review conclusion is assumed correct. Review prior artifacts as dated evidence only. This scaffolding was created after inventory and is excluded from its own frozen snapshot.

Third-party exceptions: minified Scalar implementation and full compressed upstream OpenAPI semantics are not exhaustively audited; inspect integration/provenance and projection-relevant schema objects and ancestors. All other owned files require full reads.
