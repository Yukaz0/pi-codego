package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// providerModelsFetchedMsg is delivered after an async GET {baseUrl}/models.
type providerModelsFetchedMsg struct {
	Provider string
	Models   []string
	Err      error
}

// modelsCachePath is where pi-go stores model lists discovered by querying
// provider endpoints (custom providers have no entry in models-store.json).
func modelsCachePath() string {
	dir := piAgentDirTUI()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "pi-go-models-cache.json")
}

func loadModelsCache() map[string][]string {
	path := modelsCachePath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m map[string][]string
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	return m
}

func saveModelsCacheEntry(provider string, models []string) {
	path := modelsCachePath()
	if path == "" {
		return
	}
	m := loadModelsCache()
	if m == nil {
		m = map[string][]string{}
	}
	m[strings.ToLower(provider)] = models
	if data, err := json.MarshalIndent(m, "", "  "); err == nil {
		_ = os.WriteFile(path, data, 0o644)
	}
}

// defaultBaseURLs mirrors provider.ResolveProvider fallback endpoints so a
// logged-in built-in provider can be queried even without an explicit URL.
var defaultBaseURLs = map[string]string{
	"openai":      "https://api.openai.com/v1",
	"openrouter":  "https://openrouter.ai/api/v1",
	"groq":        "https://api.groq.com/openai/v1",
	"ollama":      "http://localhost:11434/v1",
	"deepseek":    "https://api.deepseek.com/v1",
	"opencode-go": "https://opencode.ai/zen/go/v1",
}

// fetchProviderModels queries GET {baseUrl}/models (OpenAI-compatible) for a
// provider using credentials from auth.json. Returns the sorted model ids.
func fetchProviderModels(provider string) ([]string, error) {
	prov := strings.ToLower(strings.TrimSpace(provider))
	if prov == "anthropic" || prov == "gemini" || prov == "google" {
		return nil, fmt.Errorf("%s endpoint does not support /models listing", prov)
	}
	baseURL, key := "", ""
	if saved, err := loadPiAuthMap(); err == nil {
		if rec, ok := saved[prov]; ok {
			baseURL = rec["baseUrl"]
			key = rec["key"]
		}
	}
	if baseURL == "" {
		baseURL = defaultBaseURLs[prov]
	}
	if baseURL == "" {
		return nil, fmt.Errorf("no base URL known for %s (login with --url?)", prov)
	}
	url := strings.TrimSuffix(baseURL, "/") + "/models"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("parse %s response: %w", url, err)
	}
	var ids []string
	for _, d := range payload.Data {
		if d.ID != "" {
			ids = append(ids, d.ID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// fetchProviderModelsCmd runs fetchProviderModels off the UI thread and
// caches + logs the outcome via providerModelsFetchedMsg.
func fetchProviderModelsCmd(provider string) tea.Cmd {
	return func() tea.Msg {
		models, err := fetchProviderModels(provider)
		if err == nil && len(models) > 0 {
			saveModelsCacheEntry(provider, models)
		}
		return providerModelsFetchedMsg{Provider: provider, Models: models, Err: err}
	}
}
