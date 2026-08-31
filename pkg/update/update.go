// Package update provides a lightweight self-update for pi-go.
//
// It checks GitHub Releases for the latest published tag, compares it with
// the running binary's embedded version, and if a newer release exists,
// downloads the matching asset and atomically replaces the on-disk binary.
//
// Design choices:
//   - Non-fatal: network errors or a bad response are swallowed; the agent
//     must never fail to start because of an update problem.
//   - Cooldown: an on-disk timestamp prevents hammering the GitHub API more
//     than once per hour (unauthenticated rate limits are tight).
//   - Opt-out: set PI_NO_UPDATE=1 to disable entirely.
//   - Replace-on-disk: the file is renamed over the current executable. The
//     running process keeps its existing inode, so the *next* launch runs the
//     new binary. This is the standard, safe self-update pattern.
package update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo is the GitHub repository whose releases we track.
const Repo = "Yukaz0/pi-codego"

// stampDir returns the directory holding the cooldown timestamp.
// Uses ~/.pi/agent to match where auth.json lives.
func stampDir() string {
	if d := os.Getenv("PI_GO_CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".pi", "agent")
}

// cooldownFile is the path where the last-check time is recorded.
func cooldownFile() string {
	return filepath.Join(stampDir(), "update-check.stamp")
}

// shouldCheck returns true if a fresh check is allowed (cooldown passed).
func shouldCheck() bool {
	if os.Getenv("PI_NO_UPDATE") == "1" {
		return false
	}
	data, err := os.ReadFile(cooldownFile())
	if err != nil {
		return true
	}
	t, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return true
	}
	return time.Since(time.Unix(t, 0)) > time.Hour
}

// markChecked records that a check happened now.
func markChecked() {
	if err := os.MkdirAll(stampDir(), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(cooldownFile(), []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0o644)
}

// CheckAndUpdate looks up the latest release, and if it is newer than cur,
// downloads and replaces the running binary. It returns the new version
// string, or "" if no update was performed. It is safe to call on every
// startup; internal heuristics keep it cheap and non-blocking.
func CheckAndUpdate(cur string) string {
	if cur == "" || cur == "dev" || strings.HasPrefix(cur, "dev-") {
		return "" // never self-update a dev/debug build
	}
	if !shouldCheck() {
		return ""
	}
	// markChecked even on failure to respect the cooldown.
	defer markChecked()

	latest, err := latestRelease()
	if err != nil || latest == "" {
		return ""
	}
	if !newerSemver(latest, cur) {
		return ""
	}

	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	asset := fmt.Sprintf("pi-go-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		asset += ".exe"
	}
	if err := replaceBinary(exe, latest, asset); err != nil {
		return ""
	}
	return latest
}

// replaceBinary downloads the asset for the current platform and renames it
// over exe. On success the on-disk binary is the new version.
func replaceBinary(exe, tag, asset string) error {
	url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", Repo, tag, asset)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: status %d", url, resp.StatusCode)
	}

	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".pi-go-upd-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}
	// Atomic-ish replace: rename over the running binary. The old inode stays
	// open for this process; the OS is fine with the swap.
	return os.Rename(tmpName, exe)
}

// latestRelease returns the tag_name of the newest published release.
func latestRelease() (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", Repo)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "pi-go-self-update")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gh api: status %d", resp.StatusCode)
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	return body.TagName, nil
}

// newerSemver reports whether a is a strictly greater version than b.
// Handles "v" prefixes and plain "x.y.z" (and minor short forms).
func newerSemver(a, b string) bool {
	pa := parseVer(a)
	pb := parseVer(b)
	if len(pa) == 0 || len(pb) == 0 {
		return false
	}
	for i := 0; i < len(pb); i++ {
		if i >= len(pa) {
			// a is a proper prefix of b (equal so far but shorter) -> a < b
			return false
		}
		if pa[i] > pb[i] {
			return true
		}
		if pa[i] < pb[i] {
			return false
		}
	}
	return len(pa) > len(pb)
}

// parseVer splits a version string into numeric components.
func parseVer(s string) []int {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	s = strings.SplitN(s, "-", 2)[0] // drop pre-release/build suffix
	parts := strings.Split(s, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}
