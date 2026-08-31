package modelcatalog

import (
	"strings"
	"testing"
)

func TestEmbeddedCatalogLookup(t *testing.T) {
	// well-known models must carry metadata in the generated snapshot
	cases := []struct {
		full   string
		minCtx int
	}{
		{"anthropic/claude-sonnet-4-5", 100000},
		{"openai/gpt-4o", 100000},
		{"openrouter/deepseek/deepseek-chat", 0}, // openrouter ids may contain slashes
	}
	for _, c := range cases {
		i := strings.Index(c.full, "/")
		info, ok := Lookup(c.full[:i], c.full[i+1:])
		if c.minCtx == 0 {
			continue // optional entry, just exercise the path
		}
		if !ok {
			t.Fatalf("%s: tidak ditemukan di katalog embedded", c.full)
		}
		if info.ContextWindow < c.minCtx {
			t.Fatalf("%s: ContextWindow = %d, ingin >= %d", c.full, info.ContextWindow, c.minCtx)
		}
	}
}

func TestLookupUnknown(t *testing.T) {
	if _, ok := Lookup("nosuchprovider", "no-such-model"); ok {
		t.Fatal("model tak dikenal harus return false")
	}
	if _, ok := LookupFull("nodivider"); ok {
		t.Fatal("tanpa '/' harus return false")
	}
}

func TestFormatSuffix(t *testing.T) {
	info := ModelInfo{ContextWindow: 200000, InputCost: 3, OutputCost: 15, Reasoning: true}
	s := FormatSuffix(info, true)
	if !strings.Contains(s, "200k ctx") || !strings.Contains(s, "$3/$15 in/out") || !strings.Contains(s, "reasoning") {
		t.Fatalf("suffix tak lengkap: %q", s)
	}
	if s := FormatSuffix(ModelInfo{}, false); s != "" {
		t.Fatalf("tanpa metadata harus kosong, dapat %q", s)
	}
}

func TestHumanTokens(t *testing.T) {
	cases := map[int]string{128000: "128k", 1000000: "1M", 1048576: "1048.6k", 999: "999", 150000: "150k", 128500: "128.5k"}
	for n, want := range cases {
		if got := humanTokens(n); got != want {
			t.Fatalf("humanTokens(%d) = %q, ingin %q", n, got, want)
		}
	}
}
