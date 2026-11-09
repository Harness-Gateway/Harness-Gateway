# Entire-repository EGM loop standard

## Authority and scope

Review all files in the sibling coverage manifest at the exact working-tree snapshot in the round-2 manifest, full file. Treat source, tests, tooling, workflows, installers, contracts, and current documentation as first-class targets. Existing historical reviews and reproductions describe their original commits; check their evidence/provenance without treating resolved historical defects as current defects.

Use the assigned installed EGM persona in REVIEW mode, interpreted as an audit of an implemented project. Judge actual behavior and its documented contract. Missing documentation of every private helper, speculative future features, and absent implementation-plan templates are not defects by themselves.

## Reviewer execution and isolation

Read your persona, this entire standard, the baseline, README.md, AGENTS.md, and every owned file in the coverage manifest. Follow calls across ownership boundaries when necessary. Read the changelog and diagnostics design when relevant. Read all owned tests and fixtures, not only production code. Never retrieve chat history, memory files/services, prior sessions, or parent conclusions. Fresh parent-turn context does not prove exclusion of host/project instructions.

Strictly read-only: do not write files, change Git, compile, run tests or project scripts, invoke providers, restart services, contact network services, or delegate. Decompressing fixture bytes solely for inspection is permitted. Report requested external verification to the parent. Vendored minified JavaScript and compressed upstream schemas require provenance, integration, contract and validation inspection; if their full contents cannot be inspected, explicitly record that limit. A file inventoried or hashed is not automatically reviewed.

## Standards and findings

- API/HTTP: supported OpenAI subset, exact request validation, errors, streaming/non-streaming conversion, model catalog, cancellation, request/output budgets, security boundaries, embedded documentation and generated-schema consistency.
- Providers: Codex/Copilot protocol correctness, framing, event correlation, limits, error propagation, cancellation, session/storage isolation and lifecycle. Distinguish supported workflows from documented unsupported features.
- Process/lifecycle: resource ownership, timeout/cancellation ordering, Unix process-group containment, Windows Job Object/launcher semantics, cleanup and race assumptions.
- CLI/service: configuration validation, diagnostics, executable/service identity, installation/upgrade/rollback, quoting and path handling, platform behavior and actionable failures.
- Delivery: CI gates, supported platforms, pinned/latest CLI compatibility, reproducible generation/install/public-release checks, realistic regression assertions, fixture quality and acceptance evidence boundaries.
- Documentation/examples: current accuracy, runnable prerequisites, security/operational constraints, compatibility claims, example behavior, historical-review provenance, dependency/license notices.

For every finding provide stable local ID, original severity (Blocker/Major/Nit), exact file and one-based lines, violated contract, concrete trigger, observable impact, evidence category, and a narrow correction plus regression or acceptance test. Evidence categories: static proof, plausible/unverified, existing test, or validation gap. A concrete security/destructive/fundamental supported-workflow failure is a Blocker. Material conditional correctness, compatibility, resource, usability or assurance gaps are Major. Minor wording/maintenance issues are Nit. Do not invent a required number of findings. Separate accepted scope limits from defects and propose deterministic ways to settle uncertainty.

## Completion and resolution

Return completed/not run, exact files read, each exceptional/binary/generated coverage limit, findings, uncertainties and cross-area dependencies. Missing required reads or failed reviewers make a round incomplete, not clean. The parent reconciles duplicates while preserving reviewer severities and records CONTEST, ESCALATE, PROPOSE FIX or DECLINE with evidence in goldfish_review_report.md. Code findings receive proposed code/test changes rather than being silently converted into text edits. Repairs require the EGM resolution gate. No automatic commit, push, install, authenticated inference or release follows from this review invocation.

Two consecutive rounds without unsettled Blocker/Major findings constitute convergence; incomplete rounds are not clean. Reparse target ranges every round. Stop at the resolution checkpoint each round; run another round only after explicit user confirmation. The normal cap is five rounds. Never claim that this audit proves absence of all hidden defects.

## Round 2 reading rules

The round-2 baseline and coverage manifest are the source authority. Do not substitute the round-1 coverage list or historical implementation snapshots for current source. Read current source before consulting historical findings; do not retrieve earlier conversation turns, memory or parent conclusions. Historical reports/receipts are owned by docs-examples and should be judged as dated evidence, not assumptions that a fix works. Source auditors need not read historical reports unless required for a concrete current reference. The two third-party dependency limits are explicit: audit Scalar integration/provenance and the pinned upstream schema objects used by the projection, and report that their full implementations/specification are not exhaustively reviewed. All other owned files require full reads, including added tests and platform helpers.
