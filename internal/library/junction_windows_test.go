//go:build windows

package library

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRejectSymlinkPathRejectsJunction(t *testing.T) {
	parent, target := t.TempDir(), t.TempDir()
	junction := filepath.Join(parent, "junction")
	command := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "New-Item -ItemType Junction -Path $env:AGENTHUB_TEST_JUNCTION -Target $env:AGENTHUB_TEST_TARGET | Out-Null")
	command.Env = append(os.Environ(), "AGENTHUB_TEST_JUNCTION="+junction, "AGENTHUB_TEST_TARGET="+target)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Skipf("could not create temporary junction: %v: %s", err, out)
	}
	info, err := os.Lstat(junction)
	if err != nil {
		t.Fatalf("mklink reported success but junction is missing: %v", err)
	}
	if info.Mode()&os.ModeIrregular == 0 {
		t.Fatalf("Go did not identify the junction as irregular: %v", info.Mode())
	}
	if err := rejectSymlinkPath(filepath.Join(junction, "child")); err == nil {
		t.Fatal("junction in path was accepted")
	}
	if _, err := resourceFiles(parent); err == nil {
		t.Fatal("resource import followed a junction inside a bundle")
	}
	if _, err := hashTree(parent); err == nil {
		t.Fatal("resource hashing followed a junction inside a bundle")
	}
	destination := filepath.Join(t.TempDir(), "copy")
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(parent, destination); err == nil {
		t.Fatal("resource copy followed a junction inside a bundle")
	}
}
