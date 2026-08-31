# pi-code-golang

> **Rewrite native Go dari Pi Coding Agent** — ringan, cepat, hemat RAM (~15-30 MB), startup instan, multi-provider LLM, eksekusi tool native, session tree JSONL, TUI interaktif Bubbletea, mode CLI print, dan mode RPC headless.

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Binary Size](https://img.shields.io/badge/binary-~21MB-blue)](#build)
[![Code Intelligence](https://img.shields.io/badge/codebase--memory--mcp-enabled-blue)](#development)

**Asli (Node.js Pi):** [@earendil-works/pi-coding-agent](https://github.com/earendil-works/pi-coding-agent) — implementasi ini adalah port penuh ke Go tanpa runtime Node/Bun, ideal untuk edge, container minimal, dan deployment low-RAM.

---

## Daftar Isi

- [Kenapa Go?](#kenapa-go)
- [Fitur](#fitur)
- [Arsitektur](#arsitektur)
- [Instalasi](#instalasi)
- [Quick Start](#quick-start)
- [Penggunaan](#penggunaan)
  - [Interactive TUI](#1-interactive-tui-mode-default)
  - [Print Mode](#2-print-mode-one-shot)
  - [RPC Mode](#3-rpc-mode-headless-jsonl)
- [Provider LLM](#provider-llm)
- [Tools Bawaan](#tools-bawaan)
- [Context & Skills](#context--skills)
- [Session Management](#session-management)
- [Konfigurasi](#konfigurasi)
- [Struktur Project](#struktur-project)
- [Development](#development)
- [Perbandingan Node.js vs Go](#perbandingan-nodejs-vs-go)
- [Roadmap](#roadmap)

---

## Kenapa Go?

| Aspek | Node.js Pi | pi-code-golang |
|-------|------------|----------------|
| RAM idle | ~120–250 MB | **~15–30 MB** |
| Startup | ~800 ms (Bun/Node) | **< 80 ms** |
| Binary | butuh runtime | **single binary 21 MB** (`-ldflags="-s -w"`) |
| Eksekusi tool | `child_process` JS | `os/exec` native + `context` cancel |
| Distribusi | `npm` + runtime | `go build` / Docker scratch |

---

## Fitur

- **Multi-Provider Streaming** — OpenAI (+ Ollama, Groq, OpenRouter, DeepSeek), Anthropic Claude SSE, Google Gemini SSE. Auto-resolve dari string model (`openai/gpt-4o`, `ollama/llama3`, `claude-3-5-sonnet`).
- **Agent Loop + Steering** — Loop `Turn → stream → tool → Turn` dengan *live steering* (inject prompt di tengah eksekusi) dan *follow-up queue* via `SteeringController` (channel + `context.CancelFunc`).
- **4 Tools Native** — `read_file` (slice 1-indexed + cap 2000 baris), `write_file` (mkdir -p), `edit_file` (exact match, `allow_multiple`), `bash` (timeout + cwd + combine stdout/stderr).
- **Session Tree** — Struktur pohon (`Tree` + `Node`) dengan branching (`SwitchBranch`, `Rewind`), persistensi JSONL (`storage.go`), dan *compaction* otomatis jika `len(history) > MaxMessages`.
- **Context Engineering** — `AGENTS.md`/`CLAUDE.md`/`GEMINI.md` hierarchical (walk cwd → `/` hingga 10 level), override `SYSTEM.md`, dan `SkillLoader` untuk `SKILL.md` (frontmatter `name`/`description`).
- **Tiga Interface** — TUI Bubbletea + Glamour, CLI print `pi -p "query"`, RPC JSONL `pi --mode rpc`.
- **Ringan & Cepat** — Build `CGO_ENABLED=0` untuk static binary, `go vet` bersih.

---

## Arsitektur

```
┌─────────────┐     ┌──────────────┐     ┌─────────────┐
│  TUI (tea)  │     │  CLI print   │     │  RPC stdin/ │
│  viewport + │     │  -p "query"  │     │  stdout     │
│  textarea   │     └──────┬───────┘     └──────┬──────┘
└──────┬──────┘            │                    │
       └───────────────────┼────────────────────┘
                           ▼
                    ┌─────────────┐
                    │   Engine    │◄─── SteeringController (cancel/queue)
                    │  RunTurn()  │────► Tools.Registry (read/write/edit/bash)
                    └──────┬──────┘
                           │ StreamEvent (content_delta / tool_call_done / done)
              ┌────────────┼────────────┐
              ▼            ▼            ▼
           OpenAI      Anthropic     Gemini
           client      client        client
              └────────────┼────────────┘
                           ▼
                    ┌─────────────┐
                    │   Session   │──► Tree (DAG) + Storage (JSONL) + Compactor
                    └─────────────┘
                           ▲
                    ┌─────────────┐
                    │   Context   │──► system_prompt.go + agents_md.go + skills.go
                    └─────────────┘
```

**Paket:** `pkg/types` · `pkg/provider` · `pkg/tools` · `pkg/context` · `pkg/session` · `pkg/agent` · `pkg/rpc` · `pkg/tui` · `cmd/pi`

---

## Instalasi

### Linux / macOS (one-liner, tanpa Go)

```bash
curl -fsSL https://raw.githubusercontent.com/Yukaz0/pi-codego/main/scripts/install.sh | bash
```

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/Yukaz0/pi-codego/main/scripts/install.ps1 | iex
```

Installer mengunduh binary terbaru dari GitHub Releases ke `~/.local/bin/pi-go` (Linux/macOS) atau `%LOCALAPPDATA%\Programs\pi-go\pi-go.exe` (Windows).

Atau download manual di: https://github.com/Yukaz0/pi-codego/releases/latest

### Prasyarat (build dari source)

- Go **1.26+** (`go version`)
- API key salah satu provider (atau Ollama lokal tanpa key)

### Build dari Source

```bash
git clone https://github.com/Yukaz0/pi-codego
cd pi-codego

# binary optimized (~21 MB)
go build -ldflags="-s -w" -o bin/pi ./cmd/pi
ls -lh bin/pi

# atau install ke $GOPATH/bin
go install ./cmd/pi
```

### Docker (scratch, < 25 MB)

```dockerfile
FROM golang:1.26-alpine AS build
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /pi ./cmd/pi

FROM scratch
COPY --from=build /pi /pi
ENTRYPOINT ["/pi"]
```

---

## Quick Start

```bash
# 1. Set API key (pilih salah satu)
export OPENAI_API_KEY="sk-..."
# export ANTHROPIC_API_KEY="sk-ant-..."
# export GEMINI_API_KEY="AIza..."
# export OPENROUTER_API_KEY="sk-or-..."

# 2. Jalankan TUI interaktif (default)
./bin/pi --model openai/gpt-4o-mini

# 3. Atau one-shot print
./bin/pi -p "jelaskan goroutine vs thread" --model openai/gpt-4o-mini

# 4. Atau Ollama lokal (tanpa API key)
ollama serve &
./bin/pi --model ollama/llama3 --provider ollama -p "tulis fibonacci di Go"
```

---

## Penggunaan

### 1. Interactive TUI Mode (default)

```bash
./bin/pi
./bin/pi --model anthropic/claude-3-5-sonnet
./bin/pi --model gemini/gemini-1.5-flash
```

**Keybinds:**

| Shortcut | Aksi |
|----------|------|
| `Enter` | Kirim prompt (saat streaming → *steer* inject) |
| `Ctrl+C` | Interrupt turn / Quit (2x) |
| `Ctrl+L` | Clear viewport & history |
| `PgUp` / `Ctrl+U` | Scroll up |
| `PgDn` / `Ctrl+D` | Scroll down |
| `?` | Toggle help |

TUI merender markdown via `glamour` (auto-style, word-wrap 100), tool calls berwarna `▶ read_file`, hasil tool `✓/✗`, dan spinner `Pi is thinking…`.

**Slash Commands (ketik `/` di dalam chat):**

| Command | Deskripsi | Contoh |
|---------|-----------|--------|
| `/help`, `/hotkeys` | Daftar command & shortcut | `/help` |
| `/model [model]` | Lihat/ganti live — marker `● current` `★ default` `♥ favorite` `✗ responses-only` — `--default` untuk simpan | `/model openai/gpt-4o`, `/model kimi-k2.6 --default`, `/model` |
| `/login [provider] [key]` | Simpan API key ke `~/.pi/agent/auth.json` (reuse pi npm) | `/login openai sk-...` |
| `/logout [provider]` | Hapus key | `/logout openai` |
| `/favorite add\|remove\|list [model]` | Kelola favorite (♥) — disimpan di `pi-go-favorites.json` | `/favorite add kimi-k2.6`, `/favorite list` |
| `/favorites`, `/fav` | Alias list favorite | `/favorites` |
| `/session` | Info session (messages, tokens, leaf) | `/session` |
| `/tree` | Lihat tree history | `/tree` |
| `/new`, `/clear` | Mulai session baru | `/new` |
| `/compact [instr]` | Manual compact context | `/compact ringkas jadi 5 poin` |
| `/export [file]` | Export JSONL | `/export sesi.jsonl` |
| `/import <file>` | Import JSONL (stub) | `/import sesi.jsonl` |
| `/resume` | List sesi lama di `~/.pi/agent/sessions/` | `/resume` |
| `/settings` | Lihat provider/model/workdir | `/settings` |
| `/copy` | Tampilkan pesan assistant terakhir | `/copy` |
| `/reload` | Reload AGENTS.md & SKILL.md | `/reload` |
| `/trust` | Cek AGENTS.md ter-load | `/trust` |
| `/mcp [list\|reload]` | Status MCP servers & tools (`mcp_<server>_<tool>`) | `/mcp`, `/mcp list` |
| `/quit`, `/q` | Keluar | `/quit` |
| `/skill:name` | Hint skill (auto-load) | `/skill:review` |

> Ganti model: `/model deepseek-v4-flash` (switch) · `/model deepseek-v4-flash --default` (jadikan default ★) · `/favorite add kimi-k2.6` (tandai ♥) · lalu `/model` untuk lihat semua marker.

### 2. Print Mode (One-Shot)

```bash
# via -p flag
./bin/pi -p "refactor pkg/tools/bash.go agar support streaming" --model openai/gpt-4o

# via --mode print + positional
./bin/pi --mode print "buat unit test untuk session tree" --model ollama/llama3 --base-url http://localhost:11434/v1

# dengan model prefix (auto-resolve provider)
./bin/pi -p "hello" --model openrouter/anthropic/claude-3.5-sonnet
```

Output langsung ke `stdout` (delta streaming), log ke `stderr`.

### 3. RPC Mode (Headless JSONL)

Digunakan editor/extension untuk komunikasi headless:

```bash
./bin/pi --mode rpc --model openai/gpt-4o-mini
# atau tanpa provider awal, client akan set_model dulu
./bin/pi --mode rpc
```

**Protokol:** tiap baris `stdin` = JSON `RPCRequest`, tiap baris `stdout` = JSON `RPCEvent`.

**Request (client → server):**

```json
{"type":"prompt","message":"buat file hello.go"}
{"type":"abort"}
{"type":"set_model","model":"ollama/llama3","base_url":"http://localhost:11434/v1"}
{"type":"new_session"}
{"type":"resume_session","session_id":"sess-abc123"}
```

**Event (server → client):**

```json
{"type":"start","session_id":"a1b2c3"}
{"type":"turn_start"}
{"type":"content_delta","delta":"Halo, saya akan..."}
{"type":"tool_start","tool":"write_file","args":"{\"path\":\"hello.go\"...}"}
{"type":"tool_end","tool":"write_file","result":"Successfully wrote 120 bytes","is_error":false}
{"type":"turn_end"}
{"type":"done"}
{"type":"error","error":"ANTHROPIC_API_KEY is not set"}
{"type":"session_created","session_id":"sess-new"}
{"type":"aborted"}
```

**Contoh pipe:**

```bash
printf '{"type":"set_model","model":"ollama/llama3","base_url":"http://localhost:11434/v1"}\n{"type":"prompt","message":"hello"}\n' \
  | ./bin/pi --mode rpc
```

---

## Provider LLM

| Provider | Model contoh | Env var | Base URL default |
|----------|--------------|---------|------------------|
| `openai` | `openai/gpt-4o`, `gpt-4o-mini` | `OPENAI_API_KEY` | `https://api.openai.com/v1` |
| `openrouter` | `openrouter/anthropic/claude-3.5-sonnet` | `OPENROUTER_API_KEY` | `https://openrouter.ai/api/v1` |
| `groq` | `groq/llama3-70b-8192` | `GROQ_API_KEY` | `https://api.groq.com/openai/v1` |
| `ollama` | `ollama/llama3`, `ollama/mistral` | *(tanpa key, isi `ollama`)* | `http://localhost:11434/v1` |
| `deepseek` | `deepseek/deepseek-chat` | `DEEPSEEK_API_KEY` | `https://api.deepseek.com/v1` |
| `anthropic` | `anthropic/claude-3-5-sonnet-20241022`, `claude-3-opus` | `ANTHROPIC_API_KEY` | `https://api.anthropic.com/v1` |
| `gemini` | `gemini/gemini-1.5-flash`, `gemini/gemini-1.5-pro` | `GEMINI_API_KEY` atau `GOOGLE_API_KEY` | `https://generativelanguage.googleapis.com/v1beta` |

**Auto-resolve:** jika `--provider` kosong, model string di-parse (`model` mengandung `/` → prefix = provider). Fallback heuristik: `claude*` → anthropic, `gemini*` → gemini, `llama/mistral/deepseek` → openai-compatible.

```bash
# semua ekuivalen
./bin/pi --model openai/gpt-4o
./bin/pi --model gpt-4o --provider openai
./bin/pi --model ollama/llama3 --provider ollama --base-url http://localhost:11434/v1
```

---

## Tools Bawaan

| Tool | Deskripsi | Argumen | Catatan |
|------|-----------|---------|---------|
| `read_file` | Baca file dengan slice baris | `path` (str), `start_line`/`end_line` (int, 1-indexed) | Buffer 1 MB/baris, cap 2000 baris + pesan truncated. Format `"%4d | content"` |
| `write_file` | Buat/overwrite file | `path` (str), `content` (str), `overwrite` (bool) | `mkdir -p` otomatis, tolak jika exists tanpa `overwrite:true` |
| `edit_file` | Replace exact string | `path` (str), `target_content` (str), `replacement_content` (str), `allow_multiple` (bool) | Hitung kemunculan, error jika `count>1` tanpa flag |
| `bash` | Eksekusi shell | `command` (str), `cwd` (str), `timeout_seconds` (int, default 120) | `bash -c`, `exec.CommandContext`, gabung stdout+stderr, `DeadlineExceeded` handling |

**Registri:** `tools.DefaultRegistry()` atau custom `Registry.Register(Tool)`. Implementasi `types.Tool` cukup 4 method: `Name()`, `Description()`, `Definition()`, `Execute(ctx, argsJSON)`.

---

## MCP (Model Context Protocol) — Client

`pi-go` adalah **MCP Client** (stdio) — connect ke MCP server apapun dan otomatis jadi tools `mcp_<server>_<tool>`.

**Config:** `~/.pi/agent/mcp.json` (dibaca juga di `.pi/mcp.json`, `.mcp.json`):

```json
{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/home/neu/Documents"]
    },
    "codebase-memory": {
      "command": "npx",
      "args": ["-y", "codebase-memory-mcp"]
    }
  }
}
```

**Verifikasi:**
```bash
cat ~/.pi/agent/mcp.json
pi-go --model opencode-go/kimi-k2.6 -p "hi" 2>&1 | head -n 5
# [mcp] connected 2 server(s), 22 tools  ← muncul di stderr

# di dalam TUI
/mcp          # status + list tools
/mcp list
/mcp reload   # reload (restart pi-go untuk full reload)
```

**Tools MCP** muncul otomatis, contoh:
- `mcp_filesystem_read_text_file`, `mcp_filesystem_write_file`, `mcp_filesystem_list_directory`
- `mcp_codebase-memory_search_graph`, `mcp_codebase-memory_trace_path`, `mcp_codebase-memory_get_code_snippet`

> **Performa:** MCP server dimuat **async di background** (timeout 30s) agar tidak memblokir startup — TUI langsung siap < 100 ms, tools MCP terdaftar beberapa detik kemudian (log `[mcp] ... ready` di stderr). Gunakan `--no-mcp` untuk mematikan sepenuhnya.

```bash
# contoh prompt yang pakai MCP
pi-go -p "list files in /home/neu/Documents/code/pribadi/pi-code-golang using filesystem"
# → LLM akan panggil mcp_filesystem_list_directory
```

Disable server: tambah `"disabled": true` di config. ENV di `env` map akan di-merge ke `os.Environ()`.

---

## Context & Skills

### `AGENTS.md` Hierarchical Loader

`InstructionLoader.WorkspaceDir` → walk ke root (`/`) hingga 10 level:

```
./AGENTS.md
./CLAUDE.md
./GEMINI.md
./.agents.md
~/.pi/agent/AGENTS.md   (global fallback)
./SYSTEM.md              (override system prompt)
```

Semua file digabung: `=== Rules from <path> ===\n<content>`.

### `SYSTEM.md` Override

Jika `SYSTEM.md` ada di workspace, ia **mengganti** `DefaultSystemPrompt()` (yang berisi `GOOS/GOARCH`, `cwd`, dan 4 guidelines token-efficient).

### Agent Skills (`SKILL.md`)

`SkillLoader` scan:

```
./.agents/skills/<nama>/SKILL.md
./skills/<nama>/SKILL.md
~/.pi/skills/<nama>/SKILL.md
~/.gemini/config/plugins/<nama>/SKILL.md
```

Format `SKILL.md` dengan frontmatter:

```markdown
---
name: my-skill
description: Lakukan X dengan cepat
---
# Instruksi skill...
```

`FormatSkillsPrompt(skills)` akan append ke system prompt: `Available Agent Skills:\n- name: desc (path)`.

---

## Session Management

### Tree Structure

```go
tree := session.NewTree()
tree.AddMessage(types.NewUserMessage("hi"))       // Node ParentID = CurrentLeafID
tree.AddMessage(types.NewAssistantMessage("halo", nil))
history := tree.GetLinearHistory("")              // travers leaf → root, reverse → kronologis
tree.SwitchBranch(nodeID)                         // pindah cabang
tree.Rewind(2)                                    // mundur N langkah
```

**Branching:** `AddMessage` selalu child dari `CurrentLeafID`. `SwitchBranch` + `AddMessage` ciptakan cabang alternatif tanpa hapus history lama.

### JSONL Persistence

```go
storage := session.NewStorage("~/.pi/sessions") // default
storage.SaveSession("sess-123", tree)           // tiap Node → baris {"type":"node", ...} + meta
loaded, _ := storage.LoadSession("sess-123")
```

File: `~/.pi/sessions/<sessionID>.jsonl` (1 JSON per baris, scanner buffer 10 MB).

### Compaction

```go
compactor := session.NewCompactor(40, provider, "gpt-4o-mini")
// trigger jika len(history) > 40
compacted, didCompact, _ := compactor.CompactIfNeeded(ctx, history)
// compacted = [system summary "Summary of earlier conversation:\n- [user]: ..."] + recent (Max/2)
```

Saat ini ringkasan via truncasi 100 char per message (tanpa LLM call tambahan). Siap di-upgrade ke summarization LLM jika `Provider` diset.

---

## Konfigurasi

**Flags CLI (`cmd/pi/main.go` via `cobra`):**

```
--model string       Model (e.g. openai/gpt-4o, ollama/llama3)
--provider string    Override provider
--base-url string    Custom OpenAI-compatible base URL
--api-key string     API key inline (prefer env var)
-p, --print string   Print mode query
--mode string        interactive | print | rpc (default interactive)
--no-mcp             Skip MCP servers (faster startup, fewer prompt tokens)
```

**Environment:**

```bash
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
GEMINI_API_KEY=...
GOOGLE_API_KEY=...           # fallback Gemini
OPENROUTER_API_KEY=sk-or-...
GROQ_API_KEY=gsk_...
DEEPSEEK_API_KEY=sk-...
PI_MODEL=openai/gpt-4o-mini  # default jika --model kosong
```

**Otomatis baca config `pi` npm (tanpa `export` ulang):**

`pi-go` otomatis fallback ke `~/.pi/agent/auth.json`, `settings.json`, dan `models-store.json` milik `pi` npm jika `env`/`flag` kosong. Jadi jika kamu sudah `pi login` / `pi auth` di versi npm, `pi-go` langsung jalan tanpa set key lagi:

```bash
# npm pi sudah login (opencode-go/kimi-k2.6)
cat ~/.pi/agent/auth.json          # {"opencode-go": {"key": "sk-..."}}
cat ~/.pi/agent/settings.json      # {"defaultProvider": "opencode-go", "defaultModel": "kimi-k2.6"}

# pi-go langsung pakai, tidak perlu export
pi-go -p "halo"                   # → pakai opencode-go/kimi-k2.6 otomatis
pi-go --model deepseek-v4-flash -p "halo"  # model lain di provider yang sama
```

Prioritas key: `flag --api-key` > `env var` > `~/.pi/agent/auth.json` > error. BaseURL juga diambil dari `models-store.json` (`https://opencode.ai/zen/go/v1` untuk opencode).

---

## Struktur Project

```
pi-code-golang/
├── cmd/pi/main.go              # Entrypoint cobra: mode router (tui/print/rpc)
├── pkg/
│   ├── types/                  # Role, Message, ToolCall, Provider interface, StreamEvent
│   ├── provider/
│   │   ├── factory.go          # ResolveProvider (model/prefix → client + modelName)
│   │   ├── openai/client.go    # SSE OpenAI-compatible (10 MB buffer, tool accumulator)
│   │   ├── anthropic/client.go # Anthropic SSE (tool_use + input_json_delta)
│   │   └── gemini/client.go    # Gemini REST/SSE
│   ├── tools/                  # registry + read/write/edit/bash
│   ├── context/                # system_prompt + agents_md hierarchical + skills
│   ├── session/                # tree + storage JSONL + compaction
│   ├── agent/                  # engine (RunTurn loop) + steering (cancel/queue)
│   ├── rpc/                    # protocol.go + server.go (JSONL stdin/stdout)
│   └── tui/                    # model.go (tea) + view.go (lipgloss/glamour) + keybinds.go
├── docs/implementation_plan.md # Rencana implementasi (8 tasks)
├── go.mod / go.sum
├── README.md                   # (file ini)
└── bin/pi                      # binary built (gitignored)
```

---

## Development

### Verifikasi

```bash
# vet & build
 go vet ./...
 go build -o bin/pi ./cmd/pi
 ls -lh bin/pi        # ~21 MB

# codebase-memory-mcp untuk audit struktural
 # list_projects -> search_graph -> trace_path -> detect_changes
```

> **Catatan:** file `*_test.go` sengaja dihapus sesuai permintaan. Verifikasi mengandalkan `go vet`, `go build`, dan `codebase-memory-mcp`.

### Build

```bash
# dev (cepat)
go build -o bin/pi ./cmd/pi

# release (kecil, tanpa dwarf)
go build -ldflags="-s -w" -o bin/pi ./cmd/pi
ls -lh bin/pi        # ~21 MB
ps aux | grep bin/pi # cek RSS ~15-30 MB saat TUI idle

# static untuk scratch
CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/pi ./cmd/pi
```

### Lint & Code Intelligence

```bash
go vet ./...
```

**Codebase Memory MCP** — gunakan untuk eksplorasi struktural tanpa `grep` berulang (hemat ~500 token vs 80K):

```bash
# Via MCP (codebase-memory-mcp) — tersedia sebagai skill di Pi
# Contoh workflow:
# 1. list_projects                 → cek project ter-index
# 2. get_graph_schema              → pahami node/edge types
# 3. search_graph(name_pattern=".*Engine.*") → cari symbol
# 4. trace_path(function_name="RunTurn", direction="both", depth=3) → trace call chain
# 5. detect_changes()              → mapping git diff ke symbol terdampak
```

| Pertanyaan | Tool MCP |
|------------|----------|
| Siapa memanggil X? | `trace_path(direction="inbound")` |
| X memanggil apa? | `trace_path(direction="outbound")` |
| Konteks lengkap | `trace_path(direction="both")` |
| Cari pola nama | `search_graph(name_pattern="...")` |
| Dead code | `search_graph(max_degree=0)` |
| Dampak perubahan lokal | `detect_changes()` |

---

## Perbandingan Node.js vs Go

**Yang dipertahankan:** skema tool JSON, SSE streaming, session JSONL, AGENTS.md/SKILL.md, mode TUI/print/RPC, steering & compaction.

**Yang berubah:**

| Node.js Pi | Go Pi |
|------------|-------|
| Extension TS dinamis (`require` runtime) | `Skill` (`SKILL.md` + scripts) tanpa beban memori + `Go Tool Registry` (compile-time, type-safe) |
| `npm` / `bun` runtime | Single binary, `os/exec`, `net/http` stdlib |
| `JSONL` via `fs` async | `bufio.Scanner` + `sync.RWMutex` tree, buffer 10 MB |
| Prompt template `.pi/prompts/*.md` (JS) | Tetap didukung via `AGENTS.md` + `SkillLoader` path |

---

## Roadmap

- [x] Task 1 — Tipe inti (`types`)
- [x] Task 2 — Tools (`read_file`/`write_file`/`edit_file`/`bash`)
- [x] Task 3 — Provider streaming (OpenAI/Anthropic/Gemini + factory)
- [x] Task 4 — Context & Skills loader
- [x] Task 5 — Session tree + JSONL + compaction
- [x] Task 6 — Agent engine + steering
- [x] Task 7 — RPC JSONL server
- [x] Task 8 — TUI Bubbletea + CLI print + `cmd/pi`
- [x] Prompt template `.pi/prompts/*.md` + slash command custom
- [x] Tool tambahan (`glob`, `grep`, `todo`) via registry
- [x] Compaction LLM-based (summarization via provider)
- [x] Session resume via CLI flag `--session <id>`
- [x] Slash command suggestions (live autocomplete saat mengetik `/`)
- [x] `/login` custom endpoint (`--url`) + custom model (`--model`), provider apa pun via OpenAI-compatible
- [x] Session fork/clone/name/export, editor `@file`, bash mode `!cmd`/`!!cmd`
- [x] Image paste via clipboard (`Ctrl+V`: wl-paste / xclip / path file gambar)
- [x] Keybindings custom via `settings.json` → `"keybindings": { action: key }`
- [x] CI workflow (gofmt/vet/test/build per push & PR)

---

## Lisensi

MIT — lihat [LICENSE](LICENSE).

## Kredit

- **Original Pi:** [@earendil-works/pi-coding-agent](https://github.com/earendil-works/pi-coding-agent)
- **TUI:** [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea), [lipgloss](https://github.com/charmbracelet/lipgloss), [bubbles](https://github.com/charmbracelet/bubbles), [glamour](https://github.com/charmbracelet/glamour)
- **CLI:** [spf13/cobra](https://github.com/spf13/cobra)

---

*Dibuat dengan Go 1.26 · Dibangun untuk kecepatan, kesederhanaan, dan jejak memori minimal.*
