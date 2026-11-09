package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestOwnedCommandExitAndStartupErrors(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"0", "7"} {
		t.Run(code, func(t *testing.T) {
			cmd := exec.Command(executable)
			cmd.Env = append(os.Environ(), "HARNESS_GATEWAY_ACCEPTANCE_TEST_EXIT="+code)
			err := runCommand(t.Context(), cmd)
			if code == "0" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 7 {
					t.Fatalf("lost exit status: %v", err)
				}
			}
		})
	}
	cmd := exec.Command(t.TempDir() + "/missing-command")
	if err := runCommand(t.Context(), cmd); err == nil || cmd.Process != nil {
		t.Fatalf("startup failure not preserved: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cmd = exec.Command(executable)
	if err := runCommand(ctx, cmd); !errors.Is(err, context.Canceled) || cmd.Process != nil {
		t.Fatalf("started a canceled command: %v", err)
	}
	cmd = exec.CommandContext(t.Context(), executable)
	if err := runCommand(t.Context(), cmd); err == nil || !strings.Contains(err.Error(), "owner-managed") || cmd.Process != nil {
		t.Fatalf("accepted competing command cancellation: %v", err)
	}
}
