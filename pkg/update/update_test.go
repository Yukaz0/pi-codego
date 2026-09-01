package update

import (
	"testing"
)

func TestNewerSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v1.2.3", "v1.2.2", true},
		{"v1.2.3", "v1.2.3", false},
		{"v1.2.3", "v1.2.4", false},
		{"v2.0.0", "v1.9.9", true},
		{"v0.2.1", "v0.2.0", true},
		{"v0.2.1", "v0.2.1", false},
		{"v0.3.0", "v0.2.9", true},
		{"v1.0.0", "dev", false},
		{"v0.2.1", "v0.2", true},  // a has more components
		{"v0.2", "v0.2.1", false}, // b has more components
		{"1.2.10", "1.2.9", true}, // no v prefix, numeric compare
		{"v0.1.0", "v0.1", true},
	}
	for _, c := range cases {
		got := newerSemver(c.a, c.b)
		if got != c.want {
			t.Errorf("newerSemver(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestParseVer(t *testing.T) {
	if got := parseVer("v1.2.3"); len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("parseVer(v1.2.3) = %v", got)
	}
	if got := parseVer("v0.2.1-beta"); len(got) != 3 || got[0] != 0 {
		t.Fatalf("parseVer(v0.2.1-beta) = %v", got)
	}
	if got := parseVer("garbage"); got != nil {
		t.Fatalf("parseVer(garbage) = %v, want nil", got)
	}
}

func TestUpdateRejectsDevBuild(t *testing.T) {
	if _, err := Update("dev"); err == nil {
		t.Fatal("Update('dev') harus error, bukan silent no-op")
	}
}

func TestCheckAndUpdateSkipsDev(t *testing.T) {
	if got := CheckAndUpdate("dev"); got != "" {
		t.Fatalf("dev should never self-update, got %q", got)
	}
	if got := CheckAndUpdate("v0.2.1"); got != "" {
		// no cooldown stamp + reachable network: either returns "" (no newer) or the tag.
		// Do not assert a value here; assert it doesn't panic.
		_ = got
	}
}
