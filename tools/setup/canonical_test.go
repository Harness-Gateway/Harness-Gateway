package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupRejectsColonAfterResolvingSymlink(t *testing.T) {
	f := newSetupFixture(t)
	physical := filepath.Join(f.home, "physical:bin")
	if err := os.Mkdir(physical, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(f.home, "safe-alias")
	if err := os.Symlink(physical, alias); err != nil {
		t.Fatal(err)
	}
	output, err := f.run(t, "--bin-dir", alias)
	if err == nil || !strings.Contains(output, "resolved installation directory cannot contain a colon") {
		t.Fatalf("canonical PATH separator accepted: %v\n%s", err, output)
	}
	for _, path := range []string{f.log, filepath.Join(f.home, ".bashrc"), filepath.Join(physical, "harness-gateway")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("rejected path changed %s: %v", path, err)
		}
	}
}
