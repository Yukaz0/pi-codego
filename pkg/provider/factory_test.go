package provider

import (
	"os"
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
