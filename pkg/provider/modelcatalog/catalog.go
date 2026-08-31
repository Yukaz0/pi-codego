// Package modelcatalog provides curated per-model metadata (context window,
// max output tokens, cost, capabilities) for pi-go. The embedded table is
// generated from models.dev (see scripts/generate_model_catalog.go); user
// data from ~/.pi/agent/models-store.json and the live /models discovery
// cache can be layered on top via Lookup.
package modelcatalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ModelInfo is metadata for one provider/model pair.
type ModelInfo struct {
	Provider      string  `json:"provider"`
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	ContextWindow int     `json:"contextWindow"`
	MaxOutput     int     `json:"maxOutput"`
	InputCost     float64 `json:"inputCost"`  // USD per 1M input tokens
	OutputCost    float64 `json:"outputCost"` // USD per 1M output tokens
	Reasoning     bool    `json:"reasoning"`
	ImageInput    bool    `json:"imageInput"`
}

// Key is the canonical "provider/id" (lowercase) identifier.
func (m ModelInfo) Key() string {
	return strings.ToLower(m.Provider + "/" + m.ID)
}

// generatedCatalog is embedded from catalog_gen.go (models.dev snapshot).
var _ = generatedCatalog

var (
	indexOnce sync.Once
	index     map[string]ModelInfo
)

func buildIndex() {
	index = make(map[string]ModelInfo, len(generatedCatalog))
	for _, m := range generatedCatalog {
		index[m.Key()] = m
	}
}

// CatalogModels returns all embedded catalog entries as "provider/id" strings
// (sorted). Used to seed the /model picker on fresh installs where
// models-store.json is absent.
func CatalogModels() []string {
	indexOnce.Do(buildIndex)
	out := make([]string, 0, len(index))
	for k := range index {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Lookup returns metadata for "provider/model". Sources, in priority order:
//  1. ~/.pi/agent/models-store.json (Pi-compatible, richest, user-refreshed)
//  2. embedded models.dev catalog (generated)
//
// The bool is false when no metadata exists (e.g. custom providers).
func Lookup(provider, model string) (ModelInfo, bool) {
	key := strings.ToLower(provider + "/" + model)
	if info, ok := lookupStore(key); ok {
		return info, true
	}
	indexOnce.Do(buildIndex)
	info, ok := index[key]
	return info, ok
}

// LookupFull accepts a combined "provider/model" string.
func LookupFull(full string) (ModelInfo, bool) {
	i := strings.Index(full, "/")
	if i <= 0 {
		return ModelInfo{}, false
	}
	return Lookup(full[:i], full[i+1:])
}

// --- models-store.json layer -------------------------------------------------

type storeModel struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Provider      string   `json:"provider"`
	Reasoning     bool     `json:"reasoning"`
	ContextWindow int      `json:"contextWindow"`
	MaxTokens     int      `json:"maxTokens"`
	Input         []string `json:"input"`
	Cost          struct {
		Input  float64 `json:"input"`
		Output float64 `json:"output"`
	} `json:"cost"`
}

var (
	storeOnce  sync.Once
	storeIndex map[string]ModelInfo
)

func storePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pi", "agent", "models-store.json")
}

func loadStore() {
	storeIndex = map[string]ModelInfo{}
	path := storePath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var raw map[string]struct {
		Models []storeModel `json:"models"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return
	}
	for prov, entry := range raw {
		for _, m := range entry.Models {
			name := m.Name
			if name == "" {
				name = m.ID
			}
			info := ModelInfo{
				Provider: strings.ToLower(prov), ID: m.ID, Name: name,
				ContextWindow: m.ContextWindow, MaxOutput: m.MaxTokens,
				InputCost: m.Cost.Input, OutputCost: m.Cost.Output,
				Reasoning: m.Reasoning,
			}
			for _, in := range m.Input {
				if in == "image" {
					info.ImageInput = true
				}
			}
			storeIndex[info.Key()] = info
		}
	}
}

func lookupStore(key string) (ModelInfo, bool) {
	storeOnce.Do(loadStore)
	info, ok := storeIndex[key]
	return info, ok
}

// --- display helpers ----------------------------------------------------------

// FormatSuffix renders a compact right-hand annotation for the /model picker,
// e.g. "  128k ctx · $2.50/$10 in/out". Empty string when no metadata.
func FormatSuffix(info ModelInfo, ok bool) string {
	if !ok || info.ContextWindow <= 0 {
		return ""
	}
	var parts []string
	parts = append(parts, fmt.Sprintf("%s ctx", humanTokens(info.ContextWindow)))
	if info.InputCost > 0 || info.OutputCost > 0 {
		parts = append(parts, fmt.Sprintf("$%g/$%g in/out", info.InputCost, info.OutputCost))
	}
	if info.Reasoning {
		parts = append(parts, "reasoning")
	}
	return strings.Join(parts, " · ")
}

func humanTokens(n int) string {
	if n >= 1000000 && n%1000000 == 0 {
		return fmt.Sprintf("%dM", n/1000000)
	}
	if n >= 1000 {
		k := n / 1000
		if n%1000 == 0 {
			return fmt.Sprintf("%dk", k)
		}
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}
