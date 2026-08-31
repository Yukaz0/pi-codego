package tui

import "testing"

func TestParseLoginArgs(t *testing.T) {
	tests := []struct {
		args    string
		prov    string
		key     string
		url     string
		model   string
		wantErr bool
	}{
		{"openai sk-abc", "openai", "sk-abc", "", "", false},
		{"myhost key123 --url https://api.myhost.com/v1", "myhost", "key123", "https://api.myhost.com/v1", "", false},
		{"myhost --key k --url http://localhost:8080/v1 --model llama3", "myhost", "k", "http://localhost:8080/v1", "llama3", false},
		{"openai -u https://proxy.example/v1 sk-x", "openai", "sk-x", "https://proxy.example/v1", "", false},
		{"--url https://x.dev/v1", "", "", "https://x.dev/v1", "", false},
		{"myhost key --url ftp://bad", "", "", "", "", true},
		{"myhost key --url", "", "", "", "", true},
	}
	for _, tc := range tests {
		p, k, u, mdl, err := parseLoginArgs(tc.args)
		if tc.wantErr {
			if err == nil {
				t.Errorf("expected error for %q", tc.args)
			}
			continue
		}
		if err != nil {
			t.Errorf("unexpected error for %q: %v", tc.args, err)
			continue
		}
		if p != tc.prov || k != tc.key || u != tc.url || mdl != tc.model {
			t.Errorf("parseLoginArgs(%q) = (%q,%q,%q,%q), want (%q,%q,%q,%q)",
				tc.args, p, k, u, mdl, tc.prov, tc.key, tc.url, tc.model)
		}
	}
}
