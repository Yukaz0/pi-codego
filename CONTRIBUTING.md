# Contributing

Terima kasih sudah ingin berkontribusi ke `pi-code-golang`!

## Prasyarat

- Go 1.26+
- `make` (opsional)
- API key untuk testing provider (atau gunakan Ollama lokal)

## Alur Kerja

1. **Fork & clone**
   ```bash
   git clone https://github.com/<username>/pi-code-golang
   cd pi-code-golang
   ```

2. **Buat branch**
   ```bash
   git checkout -b feat/nama-fitur
   ```

3. **Koding**
   - Ikuti struktur `pkg/` yang ada (`types`, `provider`, `tools`, `session`, `agent`, `rpc`, `tui`)
   - Untuk tool baru: implement `types.Tool` dan daftar di `tools.Registry`
   - Untuk provider baru: implement `types.Provider` + tambah case di `provider/factory.go`

4. **Test**
   ```bash
   go test ./...
   go test -race ./...
   go vet ./...
   ```

5. **Build & coba manual**
   ```bash
   make build
   ./bin/pi -p "test" --model ollama/llama3 --base-url http://localhost:11434/v1
   printf '{"type":"prompt","message":"hi"}\n' | ./bin/pi --mode rpc --model ollama/llama3
   ```

6. **Code intelligence (jika ubah struktur)**
   ```bash
   # gunakan codebase-memory-mcp via Pi CLI/skill
   # list_projects -> search_graph -> trace_path -> detect_changes
   ```

7. **Commit & PR**
   ```bash
   git add .
   git commit -m "feat: deskripsi singkat"
   git push origin feat/nama-fitur
   ```
   Buka PR ke `main` dengan deskripsi jelas + hasil `go test`.

## Gaya Kode

- `gofmt` wajib (`make fmt`)
- Error handling: `fmt.Errorf("...: %w", err)` untuk wrap
- Public API harus ada komentar doc (`// Foo does ...`)
- Jangan commit `bin/`, `.env`, `*.key`

## Menambah Tool Baru

```go
// pkg/tools/mytool.go
type MyTool struct{}
func (t *MyTool) Name() string { return "my_tool" }
func (t *MyTool) Description() string { return "..." }
func (t *MyTool) Definition() types.ToolDefinition { /* ... */ }
func (t *MyTool) Execute(ctx context.Context, argsJSON string) (string, error) { /* ... */ }

// pkg/tools/registry.go → DefaultRegistry()
r.Register(NewMyTool())
```

Tambah tes di `tools_test.go`.

## Menambah Provider

1. Buat `pkg/provider/<nama>/client.go` implement `Name()` + `Stream(ctx, req) (<-chan StreamEvent, error)`
2. Tangani SSE: `bufio.Scanner` + `Buffer(..., 10*1024*1024)` + `data:` parsing
3. Daftar di `factory.go` `switch providerName`

## Reporting Issues

Sertakan: Go version, OS, command, log `stderr`, dan `go test -v` output.

## Lisensi

Kontribusi berarti setuju kode dirilis di bawah MIT (lihat `LICENSE`).
