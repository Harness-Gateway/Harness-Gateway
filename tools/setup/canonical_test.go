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

func TestSetupRelativeDirectoryIgnoresCDPATH(t *testing.T) {
	for _, mode := range []string{"current", "other"} {
		t.Run(mode, func(t *testing.T) {
			f := newSetupFixture(t)
			cdpath := "."
			if mode == "other" {
				cdpath = t.TempDir()
				if err := os.Mkdir(filepath.Join(cdpath, "bin"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("CDPATH", cdpath)
			output, err := f.run(t, "--bin-dir", "bin")
			if err != nil {
				t.Fatalf("relative installation failed: %v\n%s", err, output)
			}
			physical, err := filepath.EvalSymlinks(filepath.Join(f.home, "bin"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(physical, "harness-gateway")); err != nil {
				t.Fatalf("requested executable absent: %v\n%s", err, output)
			}
			rc, err := os.ReadFile(filepath.Join(f.home, ".bashrc"))
			if err != nil || !strings.Contains(string(rc), "export PATH="+physical+":$PATH\n") {
				t.Fatalf("wrong canonical PATH entry: %s %v", rc, err)
			}
			if mode == "other" {
				if _, err := os.Stat(filepath.Join(cdpath, "bin", "harness-gateway")); !os.IsNotExist(err) {
					t.Fatalf("CDPATH redirected the install: %v", err)
				}
			}
		})
	}
}
