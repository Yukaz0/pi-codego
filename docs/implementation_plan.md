# Full Native Golang Pi Coding Agent Core (`pi-code-golang`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a lightweight, high-performance, full native Golang rewrite of the Pi Coding Agent core with minimal RAM footprint (~15-30MB), instant startup, multi-provider LLM support, native tool execution, tree-structured JSONL session history, interactive Bubbletea TUI, CLI print mode, and headless JSONL RPC mode.

**Architecture:** A clean modular Go architecture with pluggable LLM providers (OpenAI/Compatible, Anthropic, Gemini, Ollama), an autonomous tool execution agent loop with live steering/cancellation channels, a tree-based session manager, context loader (`AGENTS.md`/Skills), and multiple interfaces (TUI, CLI, RPC).

**Tech Stack:** 
- Go 1.26+ (Standard library `net/http`, `os/exec`, `bufio`, `encoding/json`, `context`)
- TUI: `github.com/charmbracelet/bubbletea`, `github.com/charmbracelet/lipgloss`, `github.com/charmbracelet/bubbles`, `github.com/charmbracelet/glamour`
- CLI Flags: `github.com/spf13/cobra` / standard `flag`

---

## User Review Required

> [!IMPORTANT]
> **Dynamic Extensions Strategy**: In Node.js Pi, extensions are written in TypeScript and loaded dynamically at runtime. For this native Go implementation, we provide:
> 1. **Agent Skills** (`SKILL.md` + scripts) natively loaded from project/global directory (zero memory overhead).
> 2. **Prompt Templates & Custom Slash Commands** (`.pi/prompts/*.md`).
> 3. **Go Tool Registry** for easily compiling custom tools into the binary.

---

## Proposed Project Structure

```
pi-code-golang/
├── go.mod
├── go.sum
├── cmd/
│   └── pi/
│       └── main.go                  # Main entrypoint (flags, mode router: interactive, print, rpc)
├── pkg/
│   ├── types/
│   │   ├── message.go               # Role, Message, ToolCall, ToolResult definitions
│   │   ├── tool.go                  # Tool interface and Schema types
│   │   └── provider.go              # Provider interface, StreamEvent, CompletionOpts
│   ├── provider/
│   │   ├── factory.go               # Provider registry & model resolver
│   │   ├── openai/                  # OpenAI & OpenAI-compatible (Ollama, Groq, OpenRouter, DeepSeek)
│   │   ├── anthropic/               # Anthropic Claude SSE client
│   │   └── gemini/                  # Google Gemini REST/SSE client
│   ├── tools/
│   │   ├── registry.go              # Tool registry & dispatch
│   │   ├── read_file.go             # Sliced file viewer
│   │   ├── write_file.go            # File creator / overwriter
│   │   ├── edit_file.go             # Exact content replacer
│   │   └── bash.go                  # Command runner with real-time streaming & timeout
│   ├── context/
│   │   ├── system_prompt.go         # Minimal token-efficient default prompt
│   │   ├── agents_md.go             # AGENTS.md & SYSTEM.md hierarchical loader
│   │   └── skills.go                # Agent skills parser (SKILL.md + instructions)
│   ├── session/
│   │   ├── tree.go                  # Tree-structured history node & branch manager
│   │   ├── storage.go               # JSONL persistence & session switcher
│   │   └── compaction.go            # Context compaction & auto-summarization
│   ├── agent/
│   │   ├── engine.go                # Main agent loop (Turn, streaming, tool execution, steering)
│   │   └── steering.go              # Interrupt & follow-up queue management
│   ├── rpc/
│   │   └── server.go                # JSONL over stdin/stdout RPC protocol handler
│   └── tui/
│       ├── model.go                 # Bubbletea UI model & state
│       ├── view.go                  # Lipgloss layout & Glamour markdown renderer
│       └── keybinds.go              # Key shortcuts (Ctrl+C, Ctrl+L, Enter steering, etc.)
```

---

## Bite-Sized Implementation Tasks

### Task 1: Project Initialization & Core Data Types
**Files:**
- Create: `go.mod`
- Create: `pkg/types/message.go`
- Create: `pkg/types/tool.go`
- Create: `pkg/types/provider.go`
- Test: `pkg/types/message_test.go`

- [x] **Step 1: Initialize go module and write unit tests for message serialization**
- [x] **Step 2: Run test to verify it fails**
- [x] **Step 3: Implement core types (`Role`, `Message`, `ToolCall`, `ToolResult`, `Tool`, `Provider`)**
- [x] **Step 4: Run test to verify it passes**

---

### Task 2: Built-in Core Tools (`read_file`, `write_file`, `edit_file`, `bash`)
**Files:**
- Create: `pkg/tools/registry.go`
- Create: `pkg/tools/read_file.go`
- Create: `pkg/tools/write_file.go`
- Create: `pkg/tools/edit_file.go`
- Create: `pkg/tools/bash.go`
- Test: `pkg/tools/tools_test.go`

- [x] **Step 1: Write comprehensive test suite for all 4 tools (with edge cases: line slicing, invalid edit target, bash cancellation)**
- [x] **Step 2: Run test to verify it fails**
- [x] **Step 3: Implement `read_file`, `write_file`, `edit_file`, and `bash` (using `os/exec` with context cancellation)**
- [x] **Step 4: Run test to verify it passes**

---

### Task 3: Multi-Provider LLM Streaming Clients
**Files:**
- Create: `pkg/provider/openai/client.go`
- Create: `pkg/provider/anthropic/client.go`
- Create: `pkg/provider/gemini/client.go`
- Create: `pkg/provider/factory.go`
- Test: `pkg/provider/provider_test.go`

- [x] **Step 1: Write mock server test for SSE streaming of text chunks and tool calls**
- [x] **Step 2: Run test to verify it fails**
- [x] **Step 3: Implement OpenAI-compatible SSE client (with support for custom baseUrl/key for Ollama, Groq, OpenRouter, DeepSeek)**
- [x] **Step 4: Implement Anthropic SSE client and Gemini client**
- [x] **Step 5: Implement `factory.go` for resolving provider from model string (e.g. `openai/gpt-4o`, `anthropic/claude-3-5-sonnet`, `ollama/llama3`)**
- [x] **Step 6: Run test to verify it passes**

---

### Task 4: Context Engineering & Skills Loader
**Files:**
- Create: `pkg/context/system_prompt.go`
- Create: `pkg/context/agents_md.go`
- Create: `pkg/context/skills.go`
- Test: `pkg/context/context_test.go`

- [x] **Step 1: Write tests for hierarchical `AGENTS.md` resolution, `SYSTEM.md` override, and `SKILL.md` parser**
- [x] **Step 2: Run test to verify it fails**
- [x] **Step 3: Implement minimal system prompt, `AGENTS.md` locator, and skills directory reader**
- [x] **Step 4: Run test to verify it passes**

---

### Task 5: Tree-Structured Session Manager & JSONL Storage
**Files:**
- Create: `pkg/session/tree.go`
- Create: `pkg/session/storage.go`
- Create: `pkg/session/compaction.go`
- Test: `pkg/session/session_test.go`

- [x] **Step 1: Write test for tree branching, message rewind, JSONL serialize/deserialize, and context compaction**
- [x] **Step 2: Run test to verify it fails**
- [x] **Step 3: Implement tree node graph, branch creation, JSONL file appender/loader, and compaction algorithm**
- [x] **Step 4: Run test to verify it passes**

---

### Task 6: Agent Execution Loop & Live Steering Engine
**Files:**
- Create: `pkg/agent/steering.go`
- Create: `pkg/agent/engine.go`
- Test: `pkg/agent/engine_test.go`

- [x] **Step 1: Write unit and integration test for agent turn loop, multi-step tool calls, and steering interrupt**
- [x] **Step 2: Run test to verify it fails**
- [x] **Step 3: Implement Agent Engine with event callbacks, tool execution dispatcher, and steering/follow-up channel mechanics**
- [x] **Step 4: Run test to verify it passes**

---

### Task 7: RPC Mode (Headless JSONL over stdin/stdout)
**Files:**
- Create: `pkg/rpc/server.go`
- Create: `pkg/rpc/protocol.go`
- Test: `pkg/rpc/rpc_test.go`

- [ ] **Step 1: Write test for RPC command processing (`prompt`, `abort`, `set_model`, `new_session`) and event streaming**
- [ ] **Step 2: Run test to verify it fails**
- [ ] **Step 3: Implement standard Pi JSONL RPC protocol server**
- [ ] **Step 4: Run test to verify it passes**

---

### Task 8: Interactive TUI (Bubbletea + Lipgloss) & CLI Print Mode
**Files:**
- Create: `pkg/tui/model.go`
- Create: `pkg/tui/view.go`
- Create: `pkg/tui/keybinds.go`
- Create: `cmd/pi/main.go`

- [ ] **Step 1: Build interactive Bubbletea TUI with message stream, viewport scrolling, tool call status indicators, and input box**
- [ ] **Step 2: Implement CLI one-shot Print mode (`pi -p "query"`)**
- [ ] **Step 3: Implement `cmd/pi/main.go` supporting interactive, print, and rpc modes with flags (`--provider`, `--model`, `-p`, `--mode rpc`)**
- [ ] **Step 4: Build and test binary end-to-end**

---

## Verification Plan

### Automated Tests
- Run complete test suite: `go test -v ./...`
- Verify race conditions: `go test -race ./...`
- Measure binary size: `go build -ldflags="-s -w" -o bin/pi ./cmd/pi && ls -lh bin/pi`
- Verify memory consumption during execution: `ps aux | grep bin/pi`

### Manual Verification
1. **Interactive TUI Mode**:
   - Run `./bin/pi` -> Ask a coding question -> Verify streaming responses and TUI layout.
2. **Tool Execution**:
   - Ask Pi to read, edit, and write files in a scratch directory -> Verify exact edits and diff display.
3. **RPC Mode**:
   - Run `./bin/pi --mode rpc` -> Pipe JSON `{"type":"prompt","message":"hello"}` into stdin -> Verify JSONL events on stdout.
4. **Print Mode**:
   - Run `./bin/pi -p "Explain Go concurrency"` -> Verify direct text streaming to stdout.
