# Bounded EGM pilot review standard

Target: [diagnostics contract](../designs/diagnostics-observability.md).
Baseline: `6476397885b54ee9345b766274ae196cc3d90b90`.

## Scope and authority

Use installed EGM personas in REVIEW mode with fresh parent-turn contexts.
This is the user-approved focused pilot, not the full five-stage EGM workflow
and not a claim of convergence under its standard multi-round loop.

Review the complete target contract against baseline source and relevant tests.
The contract distinguishes desired requirements from current implementation.
Challenge both. A design requirement may be underspecified even when code and
tests agree. An observation of undocumented internal helpers is not automatically
a product defect; judge it against this scoped contract.

Read the assigned installed persona, this standard, the entire target document,
and all explicitly supplied source files before returning findings. Source search
may follow relevant calls, but remain inside this repository and installed EGM
resources. Do not retrieve past conversation history or memory services.

Reviewers are read-only. Do not write files, change Git, run builds or tests,
contact CLIs/providers, restart services, or spawn additional reviewers. Return
findings to the parent, which records reports and can reproduce alleged defects.

## Evidence requirements

For each finding include:

1. Stable local ID and severity: Blocker, Major, or Nit.
2. Requirement ID and exact source/document file and one-based line references.
3. A concrete trigger, observable effect, and why it matters within this scope.
4. Evidence category: static proof, plausible but unverified, or existing test.
5. A narrowly scoped correction or regression test that would settle it.

Blocker means a concrete security, destructive, or fundamental correctness defect
that prevents shipping this scope. Major means a material incorrect diagnostic,
outcome, lifecycle behavior, or unsupported acceptance claim. Nit means wording or
minor completeness; it does not block release. Speculative general risks are
questions rather than confirmed defects and must be labeled accordingly.

Do not propose broader APIs, multitenancy, network exposure, new telemetry
services, or exhaustive documentation of every function as prerequisites.

## Result format

Return `completed` or `not run`, the files actually read, findings grouped by
severity, and explicit verification limits. A missing required read is an
incomplete review. State no findings when appropriate; do not invent issues to
fill the report. Host/project instruction exclusion is unverified even though
the native spawn request disables inheritance of parent conversation turns.

The parent consolidates findings, records PROPOSE FIX or CONTEST with evidence,
and addresses substantiated issues within the authorized scope. At most one
fresh follow-up reviews the changes and remaining major issues. This bounds the
pilot; it does not assert the stock EGM loop's two-clean-round convergence.
