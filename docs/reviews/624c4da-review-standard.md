# Project-wide EGM audit standard

Target: [project baseline](624c4da-project-baseline.md), complete file.
Baseline commit: `624c4dafb7afd003a000ee886c3a2102744d1786`.

## Reviewer requirements

Read the installed persona assigned by the parent, this standard, the baseline,
the assigned source set, and relevant tests/docs. Work in REVIEW mode with a
fresh conversation context. Do not retrieve earlier chat, session history, or
memory. Shared host/project instructions are not guaranteed to be excluded.

Use a source-oriented interpretation of the persona: test actual implementation
and documented requirements. Do not treat missing proposed future product scope
or absent documentation for every private helper as automatic defects.

Reviewers are read-only: do not write artifacts, change Git, compile/run tests,
run providers, restart services, or delegate further. Return reports to the parent.
The parent alone may run local-fixture probes and save evidence. Follow calls
across area boundaries when necessary; name files actually inspected.

## Findings

For every issue provide a stable reviewer-local ID, priority P0-P3, exact path
and one-based line references, violated contract, concrete trigger, observable
impact, evidence category, and narrow fix/test recommendation.

- P0: immediate, broadly reachable destructive or security-critical failure.
- P1: material safety/correctness/resource failure in a supported workflow.
- P2: meaningful but conditional correctness, compatibility, usability, or
  acceptance gap that should be fixed.
- P3: low-impact clarity/maintenance problem; do not bury material findings in nits.

Evidence categories: reproduced (only if actually executed), static proof,
plausible/unverified, or validation gap. Explain reachability and prerequisites.
Do not claim exploitation, process leakage, privilege escalation, or protocol
behavior from a suspicious pattern alone. Account for existing tests, limits,
runtime cleanup, and deliberately unsupported configurations.

Report intentional unsupported capabilities separately, without assigning a bug
priority. Distinguish implementation failures from absent acceptance evidence.
Findings should be falsifiable; suggest a deterministic regression or specific
manual acceptance gate. Return no findings if appropriate.

## Completion and reconciliation

Return completed/not run, the exact owned file set read, extra files inspected,
findings, uncertainties, and coverage limits. A failed/missing reviewer is not a
clean area. No requirement exists to find a particular number of issues.

The parent records ACCEPT, CONTEST, or NEEDS VALIDATION for each raw issue, with
duplicates mapped to stable consolidated IDs. Fresh final scrutiny may challenge
the consolidated findings. The final report must not promise all hidden issues
were found, describe static findings as runtime proof, or imply tests on one
platform establish acceptance on another. This audit does not automatically fix,
commit, push, release, or install project changes.
