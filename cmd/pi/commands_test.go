package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// newTestRoot builds a root command wired like main() but without the TUI,
// so subcommands can be executed in-process against an isolated HOME.
func newTestRoot() *cobra.Command {
	root := &cobra.Command{Use: "pi"}
	addCLICommands(root)
	root.SilenceUsage = true
	root.SilenceErrors = true
	return root
}

func runCmd(t *testing.T, root *cobra.Command, args ...string) (string, error) {
	t.Helper()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

// isolateHOME points HOME at a temp dir with a minimal ~/.pi layout so the
// real user config is never read or written.
func isolateHOME(t *testing.T, settings map[string]string, keys ...string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".pi", "agent")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if settings != nil {
		data, _ := json.Marshal(settings)
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if len(keys) > 0 {
		m := map[string]map[string]string{}
		for _, k := range keys {
			m[k] = map[string]string{"type": "api_key", "key": "SECRET-DO-NOT-PRINT"}
		}
		data, _ := json.Marshal(m)
		if err := os.WriteFile(filepath.Join(dir, "auth.json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestConfigCmdShowsDefaultsAndHidesKeys(t *testing.T) {
	isolateHOME(t, map[string]string{"defaultProvider": "testprov", "defaultModel": "test-model"}, "testprov")
	out, err := runCmd(t, newTestRoot(), "config")
	if err != nil {
		t.Fatalf("config: %v\n%s", err, out)
	}
	if !strings.Contains(out, "default:     testprov/test-model") {
		t.Errorf("default provider/model tidak tampil:\n%s", out)
	}
	if !strings.Contains(out, "keys saved:  testprov") {
		t.Errorf("nama provider key tidak tampil:\n%s", out)
	}
	if strings.Contains(out, "SECRET-DO-NOT-PRINT") {
		t.Errorf("RAHASIA: nilai key tercetak di output:\n%s", out)
	}
}

func TestConfigCmdNoDefaults(t *testing.T) {
	isolateHOME(t, nil)
	out, err := runCmd(t, newTestRoot(), "config")
	if err != nil {
		t.Fatalf("config: %v\n%s", err, out)
	}
	if !strings.Contains(out, "default:     (none") {
		t.Errorf("pesan default kosong tidak tampil:\n%s", out)
	}
}

func TestSessionsCmdEmptyDir(t *testing.T) {
	isolateHOME(t, nil)
	out, err := runCmd(t, newTestRoot(), "sessions")
	if err != nil {
		t.Fatalf("sessions: %v\n%s", err, out)
	}
	if !strings.Contains(out, "no saved sessions") {
		t.Errorf("sesi kosong harus dilaporkan:\n%s", out)
	}
	// Verb gaya TUI diterima.
	if _, err := runCmd(t, newTestRoot(), "sessions", "list"); err != nil {
		t.Errorf("sessions list ditolak: %v", err)
	}
}

func TestSessionsCmdRejectsUnknownVerb(t *testing.T) {
	isolateHOME(t, nil)
	out, err := runCmd(t, newTestRoot(), "sessions", "bogus")
	if err == nil {
		t.Fatalf("verb tak dikenal harus error:\n%s", out)
	}
	if !strings.Contains(err.Error(), "unknown sessions subcommand") {
		t.Errorf("pesan error salah: %v", err)
	}
}

// stubLatest replaces the GitHub lookup for the duration of a test.
func stubLatest(t *testing.T, fn func() (string, error)) {
	t.Helper()
	old := latestFn
	latestFn = fn
	t.Cleanup(func() { latestFn = old })
}

func TestUpdateCheckCmdDevBuild(t *testing.T) {
	stubLatest(t, func() (string, error) { return "v9.9.9", nil })
	out, err := runCmd(t, newTestRoot(), "update", "--check")
	if err != nil {
		t.Fatalf("update --check: %v\n%s", err, out)
	}
	if !strings.Contains(out, "development build (dev)") || !strings.Contains(out, "v9.9.9") {
		t.Errorf("build dev harus disebut eksplisit + latest:\n%s", out)
	}
}

func TestUpdateCheckCmdOutdated(t *testing.T) {
	old := version
	version = "v0.4.4"
	defer func() { version = old }()
	stubLatest(t, func() (string, error) { return "v0.5.0", nil })
	out, err := runCmd(t, newTestRoot(), "update", "--check")
	if err != nil {
		t.Fatalf("update --check: %v\n%s", err, out)
	}
	if !strings.Contains(out, "update available: v0.4.4 → v0.5.0") {
		t.Errorf("pesan update tidak tampil:\n%s", out)
	}
}

func TestUpdateCheckCmdLatest(t *testing.T) {
	old := version
	version = "v0.5.0"
	defer func() { version = old }()
	stubLatest(t, func() (string, error) { return "v0.5.0", nil })
	out, err := runCmd(t, newTestRoot(), "update", "--check")
	if err != nil {
		t.Fatalf("update --check: %v\n%s", err, out)
	}
	if !strings.Contains(out, "already on the latest version") {
		t.Errorf("pesan up-to-date tidak tampil:\n%s", out)
	}
}

func TestUpdateCmdDevBuildErrors(t *testing.T) {
	out, err := runCmd(t, newTestRoot(), "update")
	if err == nil {
		t.Fatalf("update pada build dev harus error:\n%s", out)
	}
	if !strings.Contains(err.Error(), "development build") {
		t.Errorf("pesan error salah: %v", err)
	}
}

func TestDoctorCmdHermetic(t *testing.T) {
	isolateHOME(t, map[string]string{"defaultProvider": "p1", "defaultModel": "m1"}, "p1")
	stubLatest(t, func() (string, error) { return "v9.9.9", nil })
	out, err := runCmd(t, newTestRoot(), "doctor")
	if err != nil {
		t.Fatalf("doctor: %v\n%s", err, out)
	}
	for _, want := range []string{"default model", "OK", "p1/m1", "api keys", "sessions"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output kehilangan %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "SECRET-DO-NOT-PRINT") {
		t.Errorf("RAHASIA: key tercetak:\n%s", out)
	}
}
