# Project-review probes

These probes target `624c4dafb7afd003a000ee886c3a2102744d1786`. They add tests and,
for the pre-reply burst, one fixture mode through Go's filesystem overlay. They
do not edit tracked production or test source.

Run from the repository with Go 1.27.3+:

```bash
python3 docs/reviews/624c4da-repros/run.py copilot-burst
python3 docs/reviews/624c4da-repros/run.py installer-path
python3 docs/reviews/624c4da-repros/run.py http-schema
python3 docs/reviews/624c4da-repros/run.py service-identity
python3 docs/reviews/624c4da-repros/run.py copilot-race
python3 docs/reviews/624c4da-repros/run.py acceptance-interrupt
```

At the review baseline, all except `copilot-race` intentionally fail their
correctness assertions, demonstrating the report's discrepancy. A passing race
probe is a narrow negative result, not proof of race freedom.

The installer case uses the existing fake Go installer and a temporary Bash
profile. Service identity uses fake systemctl and a temporary configuration.
Copilot cases use the Go test binary as a CLI, never account credentials or
upstream inference. Interruption uses actual Java `FixtureGateway` classes and a
Go Codex fixture; it cancels an owned process group and explicitly kills any
recorded surviving fixture groups afterward.

The interruption case is Linux-specific and requires JDK 27 plus the existing
compiled fixture classes/Surefire classpath from ordinary Spring AI acceptance.
It reproduces the acceptance cancellation helper and Java/gateway/CLI topology,
not a complete Maven invocation. Other Unix/platform prerequisites follow the
existing fixtures; Windows/Darwin reproductions were not performed.

Saved `*-result.txt` files contain the actual parent-run results. Temporary paths
and PIDs describe those past runs; they are not current process identities.
