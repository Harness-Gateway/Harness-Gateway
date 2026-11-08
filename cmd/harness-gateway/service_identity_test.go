package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceMutationsRequireManagerLoadedManagedUnit(t *testing.T) {
	for _, state := range []string{"foreign", "transient", "not-found", "masked"} {
		for _, action := range []string{"start", "stop", "restart", "enable", "disable", "uninstall"} {
			t.Run(state+"/"+action, func(t *testing.T) {
				path, log := serviceControllerFixture(t)
				unit := []byte(serviceMarker)
				if err := writeService(path, unit); err != nil {
					t.Fatal(err)
				}
				switch state {
				case "foreign":
					foreign := filepath.Join(t.TempDir(), serviceName)
					if err := os.WriteFile(foreign, []byte("# another unit"), 0600); err != nil {
						t.Fatal(err)
					}
					t.Setenv("SERVICE_TEST_FRAGMENT", foreign)
				case "transient":
					t.Setenv("SERVICE_TEST_FRAGMENT", "")
				default:
					t.Setenv("SERVICE_TEST_LOAD_STATE", state)
				}
				if err := serviceCommand(t.Context(), []string{action}, io.Discard); err == nil || !strings.Contains(err.Error(), "refusing to modify") {
					t.Fatalf("accepted mismatched manager unit: %v", err)
				}
				calls, err := os.ReadFile(log)
				if err != nil || string(calls) != "--user show --property=LoadState,FragmentPath "+serviceName+"\n" {
					t.Fatalf("unexpected controller mutation: %s %v", calls, err)
				}
				contents, err := os.ReadFile(path)
				if err != nil || string(contents) != string(unit) {
					t.Fatalf("managed file changed on rejection: %q %v", contents, err)
				}
			})
		}
	}
}
