# Entire-repository EGM loop baseline

Baseline: `73da315fc9d384595c2bf229a37abf3963fcfb82` on `dev`.

Target: all 159 tracked files, full file, including production code, tests, tooling, installers, CI, API contracts, examples, documentation, dependencies and notices.

This baseline and its sibling standard/coverage manifest are review scaffolding created after the baseline; they do not add project behavior.

Standard: [73da315-loop-standard.md](73da315-loop-standard.md).
Coverage and original full-file bounds: [73da315-loop-coverage.json](73da315-loop-coverage.json).

## Proposed review configuration

| Area | Installed EGM persona | Owned files |
| --- | --- | ---: |
| api-http | codebase_details | 30 |
| providers | codebase_details | 25 |
| process-lifecycle | design_critic | 12 |
| cli-service | codebase_details | 20 |
| delivery | implementation_readiness | 31 |
| docs-examples | comprehension | 41 |

Each auditor gets the baseline, standard, manifest, README, AGENTS, owned files, and relevant cross-area files as its reading list. Run six independent fresh-context auditors in two waves of three. Source reasoning is separated from parent-run regression checks. No parent conversation or memory is supplied.

Artifacts: review configuration and manifest in this directory; final annotated findings in `docs/reviews/goldfish_review_report.md`. Findings are reviewed before repairs; each new round requires the skill checkpoint.
