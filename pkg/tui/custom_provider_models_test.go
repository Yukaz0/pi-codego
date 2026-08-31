package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A custom provider added via /login has no entry in models-store.json, so
// /model must still list it (from auth.json) and its models (from the live
// GET {baseUrl}/models fetch cached in pi-go-models-cache.json).
func TestCustomProviderVisibleInModelPicker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// fake OpenAI-compatible endpoint serving /models
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"llama-3.1-70b"},{"id":"qwen-2.5-coder"}]}`)
	}))
	defer srv.Close()

	// simulate saved custom provider (as /login would)
	dir := filepath.Join(home, ".pi", "agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	auth := map[string]map[string]string{
		"myhost": {
			"type":    "api_key",
			"key":     "secret",
			"baseUrl": srv.URL + "/v1",
		},
	}
	data, _ := json.Marshal(auth)
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// fetchProviderModels must hit the endpoint and return sorted ids
	models, err := fetchProviderModels("myhost")
	if err != nil {
		t.Fatalf("fetchProviderModels: %v", err)
	}
	if len(models) != 2 || models[0] != "llama-3.1-70b" || models[1] != "qwen-2.5-coder" {
		t.Fatalf("model salah: %v", models)
	}
	saveModelsCacheEntry("myhost", models)

	// availableModels now includes the custom provider's models
	m := NewModel(nil)
	var found int
	for _, full := range m.availableModels() {
		if strings.HasPrefix(full, "myhost/") {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("availableModels harus menyertakan 2 model myhost, dapat %d", found)
	}

	// provider picker lists myhost even though models-store.json is absent
	m.openModelProviderPicker()
	if !m.picker.active {
		t.Fatal("picker provider tidak aktif")
	}
	hasMyhost := false
	for _, it := range m.picker.items {
		if it == "myhost" {
			hasMyhost = true
		}
	}
	if !hasMyhost {
		t.Fatalf("myhost harus muncul di daftar provider: %v", m.picker.items)
	}

	// scoped picker shows only myhost models
	m.openModelPickerScoped("myhost", "")
	for _, it := range m.picker.items {
		if !strings.HasPrefix(it, "myhost/") {
			t.Fatalf("picker harus ter-scope ke myhost, dapat %q", it)
		}
	}
	if m.picker.scope != "myhost" {
		t.Fatalf("picker.scope = %q, ingin myhost", m.picker.scope)
	}
}
