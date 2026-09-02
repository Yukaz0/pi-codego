package provider

import (
	"os"
	"path/filepath"
	"testing"
)

// Regression: model vendor "stealth" harus di-route ke OpenRouter dengan
// model id lengkap "stealth/<name>", bukan gagal "unsupported provider".
func TestResolveStealthModel(t *testing.T) {
	os.Setenv("OPENROUTER_API_KEY", "test-key")
	defer os.Unsetenv("OPENROUTER_API_KEY")

	cases := []struct {
		name      string
		cfg       Config
		wantModel string
	}{
		{
			name:      "full prefix via --model",
			cfg:       Config{Model: "openrouter/stealth/ox-alpha"},
			wantModel: "stealth/ox-alpha",
		},
		{
			name:      "PI_MODEL style: stealth/x",
			cfg:       Config{Model: "stealth/ox-alpha"},
			wantModel: "stealth/ox-alpha",
		},
		{
			name:      "explicit provider stealth",
			cfg:       Config{Provider: "stealth", Model: "ox-alpha"},
			wantModel: "stealth/ox-alpha",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pvd, model, err := ResolveProvider(tc.cfg)
			if err != nil {
				t.Fatalf("ResolveProvider(%+v): %v", tc.cfg, err)
			}
			if pvd == nil {
				t.Fatal("provider nil")
			}
			if model != tc.wantModel {
				t.Errorf("model = %q, want %q", model, tc.wantModel)
			}
		})
	}
}

// Regression: saat flag/env kosong (cfg kosong), ResolveProvider harus memuat
// defaultProvider/defaultModel dari ~/.pi/agent/settings.json (interop pi npm),
// BUKAN fallback ke model deprecate stealth/ox-alpha. Ini adalah mekanisme yang
// membuat `pi-go -p "..."` memakai default tersimpan tanpa 404.
func TestResolveProviderUsesSettingsDefault(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, ".pi", "agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	settings := `{"defaultProvider":"bai","defaultModel":"glm-5.3-flash"}`
	if err := os.WriteFile(filepath.Join(agentDir, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	auth := `{"bai":{"type":"openai","key":"test-key","baseUrl":"http://127.0.0.1:1234/v1"}}`
	if err := os.WriteFile(filepath.Join(agentDir, "auth.json"), []byte(auth), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	pvd, model, err := ResolveProvider(Config{})
	if err != nil {
		t.Fatalf("ResolveProvider(empty): %v", err)
	}
	if model != "glm-5.3-flash" {
		t.Fatalf("model = %q, want settings default \"glm-5.3-flash\" (bukan stealth/ox-alpha)", model)
	}
	if pvd == nil {
		t.Fatal("provider nil")
	}
}
