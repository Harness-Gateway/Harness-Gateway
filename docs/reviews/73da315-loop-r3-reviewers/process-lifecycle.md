# Process lifecycle — round 3 native reviewer report

Agent: `/root/loop_r3_process`; fresh context (`fork_turns: none`). Parent transcription of actual final report. Completed: all 16 owned files and mandatory material fully read; no unrecovered truncations. No writes/Git mutations/build/tests/scripts/provider/network/delegation/memory/history/other-reviewer inspection.

## process-lifecycle-r3-1 — Major

**Acceptance loses graceful descendant cleanup when the command leader exits independently.**

- Locations: tools/acceptance/main.go:75–79; internal/process/process_unix.go:46–55 and 25–43. Contrasting cancellation policy: tools/acceptance/process_unix.go:13–16.
- Trigger: Maven leader exits/crashes while a Java-owned gateway has an active CLI operation, without runner context cancellation. Deterministic fixture: TestAcceptanceGatewayOwner exits after gateway reaches catalog-gate.
- Evidence: waitCommand requests TERM/grace only through context.AfterFunc. Independent leader exit enters Child.Wait, which calls ordinary Stop and immediately SIGKILLs the Maven group. Java-owned gateways share that group; their CLIs deliberately have separate groups. Immediate gateway death prevents shutdown handlers from retiring CLI groups. Both catalog-gate fixtures wait for a file rather than stdin EOF (codex_test.go:162–180; copilot_test.go:304–322), so EOF cannot settle that fixture case.
- Impact: failed acceptance can leave gated CLI fixtures alive after failure/temporary executable removal. Installed CLI behavior after losing gateway is provider-dependent; no real-package orphan claim made.
- Contract: acceptance resource ownership/cleanup and README:510–516 runner lifecycle, with separately grouped CLIs explicitly acknowledged by process_unix.go:14–15.
- Evidence category: static proof of missing grace and fixture survival path, not runtime reproduced by reviewer.
- Narrow correction: acceptance owner retires remaining command-group members using TERM/grace before forced termination/reaping even after independently observed leader exit. Preserve immediate provider-operation cleanup.
- Regression: gated-CLI ownership test for leader status 0 and 7 with live context; gateway and CLI PIDs exit, command status preserved, existing identity reservation assertions retained.

No Blocker or Nit findings.

## Coverage

All 16 process-lifecycle paths in the round-3 inventory fully read; exact list retained in round-3 execution receipt. Common full reads: installed design_critic persona, RTK, round-3 standard/baseline/manifest, README, AGENTS. Additional full reads:

```
cmd/harness-gateway/main.go
cmd/harness-gateway/versions.go
cmd/harness-gateway/versions_process_test.go
internal/adapters/outbound/codex/codex.go
internal/adapters/outbound/codex/transport.go
internal/adapters/outbound/copilot/bridge.go
internal/adapters/outbound/copilot/bridge_linux_test.go
internal/adapters/outbound/copilot/bridge_test.go
internal/adapters/outbound/copilot/client.go
internal/adapters/outbound/copilot/copilot.go
internal/adapters/outbound/copilot/lifecycle_test.go
internal/adapters/outbound/copilot/process_unix_test.go
tools/acceptance/command_test.go
tools/acceptance/main.go
tools/acceptance/main_test.go
tools/acceptance/ownership_linux_test.go
tools/acceptance/process_other.go
tools/acceptance/process_unix.go
tools/acceptance/process_unix_test.go
examples/spring-ai/src/test/java/io/harnessgateway/FixtureGateway.java
```

Targeted reads only: codex/codex_test.go:1–240 and descendant/catalog-gate excerpts of copilot/copilot_test.go. No owned binary/generated exceptions.

Native Linux/macOS/Windows behavior not exercised. Windows Job Object closes and direct process joins, but descendants are not explicitly joined; potential Copilot state-file deletion ordering needs a native fixture holding a non-delete-sharing file. This remains uncertainty, not a finding. Documented Unix setpgid/setsid boundary accepted. Parent-turn isolation active; host/project instruction exclusion unverified.
