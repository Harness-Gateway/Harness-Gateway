# macOS exit-transition cleanup follow-up

Date: 2026-11-09. Base: `a5615186555cf6e54b8949c79baa00bb727c30e3`.

The follow-up validation passed ten jobs, including native Windows batch wrappers, but macOS Spring acceptance failed in the Codex model/reasoning contract test. The retained gateway log shows successful output followed by `terminate CLI process group 3482: operation not permitted`, converting that buffered request into HTTP 502. This is a production cleanup error, not an upstream authentication failure. [Failed job and retained artifact](https://github.com/Harness-Gateway/Harness-Gateway/actions/runs/42598961714/job/129098416361).

The prior macOS predicate took one group-state snapshot after killpg returned EPERM. Kernel signal handling and process-table inspection are separate observations during exit; XNU filters zombie group members before calculating the signal result. See the [pinned Apple kernel source](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/bsd/kern/kern_sig.c#L1612-L1621). The observed failure is consistent with a transition not yet confirmed by that single snapshot.

After EPERM, re-inspect the still-owned group for at most 250 ms. Accept cleanup only when a nonempty snapshot confirms every member is a zombie. Empty or live snapshots never count as success; inspection errors fail closed. The leader stays unreaped and no additional numeric group signal is issued during confirmation. Linux and Windows behavior is unchanged. The Unreleased changelog records this macOS correction.

Regressions cover live-to-zombie and transient-empty-to-zombie snapshots, persistent live/mixed/empty groups, and inspection failures. A native macOS integration regression races EOF-driven exit against Stop in 50 owned commands, retaining wait/cleanup ownership and rejecting cleanup errors.

Local validation: `go fmt ./...`, `go vet ./...`, `go test ./...`, `git diff --check`, and Darwin ARM64 process-test cross-compilation passed. Linux execution does not validate the Darwin kernel path; the follow-up commit's native Go and Spring AI jobs are the runtime gate. The earlier Windows-only test correction is preserved.

Changed platform-source hashes:

- `internal/process/wait_darwin.go`: `0117ed3f5ca011bbf3034e54c66d5f09f15ad3f1229c06bb738b7bdf81b2f9bb`
- `internal/process/process_darwin_test.go`: `a7d21abd751c2fae83ecf79ea22aeac767b6673a1747551ecfc828c1be24e138`
