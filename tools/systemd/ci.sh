#!/usr/bin/env bash
# Use a disposable account on the GitHub-hosted Linux runner, never the host user.
set -euo pipefail
[[ ${GITHUB_ACTIONS:-} == true ]] || { echo 'This wrapper is for disposable GitHub Actions runners only.' >&2; exit 2; }
test_user="hg-service-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"
test_root=$(mktemp -d /tmp/harness-gateway-systemd.XXXXXX)
created=false
cleanup() {
  status=$?
  cleanup_status=0
  if "$created"; then
    # The runner owns the artifact file; only journalctl runs as the test user.
    # shellcheck disable=SC2024
    sudo -u "$test_user" env XDG_RUNTIME_DIR="/run/user/$test_uid" \
      DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$test_uid/bus" \
      journalctl --user -u harness-gateway.service --no-pager > "$RUNNER_TEMP/systemd-service-journal.log" || cleanup_status=$?
    sudo loginctl disable-linger "$test_user" || cleanup_status=$?
    sudo systemctl stop "user@$test_uid.service" "user-runtime-dir@$test_uid.service" || cleanup_status=$?
    sudo userdel --remove "$test_user" || cleanup_status=$?
  fi
  sudo rm -rf -- "$test_root" || cleanup_status=$?
  if ((status == 0)); then status=$cleanup_status; fi
  exit "$status"
}
trap cleanup EXIT
go build -o "$test_root/gateway" ./cmd/harness-gateway
go test -c -o "$test_root/codex" ./internal/adapters/outbound/codex
go test -c -o "$test_root/copilot" ./internal/adapters/outbound/copilot
go test -c -o "$test_root/service.test" ./tools/systemd
sudo useradd --create-home --user-group --groups systemd-journal --shell /bin/bash "$test_user"
created=true
test_uid=$(id -u "$test_user")
sudo chown -R "$test_user:$test_user" "$test_root"
sudo loginctl enable-linger "$test_user"
sudo systemctl start "user@$test_uid.service"
sudo -u "$test_user" env HOME="/home/$test_user" \
  PATH=/usr/local/bin:/usr/bin:/bin \
  XDG_RUNTIME_DIR="/run/user/$test_uid" \
  DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$test_uid/bus" \
  HARNESS_GATEWAY_TEST_SYSTEMD=1 \
  HARNESS_GATEWAY_SYSTEMD_GATEWAY="$test_root/gateway" \
  HARNESS_GATEWAY_SYSTEMD_CODEX="$test_root/codex" \
  HARNESS_GATEWAY_SYSTEMD_COPILOT="$test_root/copilot" \
  "$test_root/service.test" -test.v -test.timeout=4m 2>&1 | tee "$RUNNER_TEMP/systemd-service.log"
