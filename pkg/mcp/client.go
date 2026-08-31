package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"pi/pkg/types"
)

// Client is a stdio MCP client for one server
type Client struct {
	Name   string
	Config ServerConfig

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	mu      sync.Mutex
	nextID  int
	pending map[int]chan rpcResponse
	tools   []*MCPTool

	readerDone chan struct{}
}

type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id,omitempty"`
	Method  string      `json:"method,omitempty"`
	Params  interface{} `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("mcp error %d: %s", e.Code, e.Message)
}

// NewClient creates but not yet started
func NewClient(name string, cfg ServerConfig) *Client {
	return &Client{
		Name:    name,
		Config:  cfg,
		pending: make(map[int]chan rpcResponse),
	}
}

func (c *Client) Start(ctx context.Context) error {
	if c.Config.Disabled {
		return fmt.Errorf("server disabled")
	}
	if c.Config.Command == "" {
		return fmt.Errorf("no command")
	}
	c.cmd = exec.CommandContext(ctx, c.Config.Command, c.Config.Args...)
	// env
	if len(c.Config.Env) > 0 {
		c.cmd.Env = os.Environ()
		for k, v := range c.Config.Env {
			c.cmd.Env = append(c.cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}
	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, _ := c.cmd.StderrPipe()
	c.stdin = stdin
	c.stdout = stdout
	c.stderr = stderr

	if err := c.cmd.Start(); err != nil {
		return err
	}
	// start reader
	c.readerDone = make(chan struct{})
	go c.readLoop()

	// drain stderr to avoid blocking
	if c.stderr != nil {
		go func() {
			sc := bufio.NewScanner(c.stderr)
			for sc.Scan() {
				// could log to stderr
			}
		}()
	}

	// initialize
	if err := c.initialize(ctx); err != nil {
		c.Close()
		return fmt.Errorf("initialize %s: %w", c.Name, err)
	}
	// list tools
	if err := c.discoverTools(ctx); err != nil {
		// not fatal, just no tools
		_ = err
	}
	return nil
}

func (c *Client) readLoop() {
	defer close(c.readerDone)
	scanner := bufio.NewScanner(c.stdout)
	// 10MB for MCP messages
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}
		// if it's a request/notification from server (no ID or method), ignore for now
		if resp.ID == 0 && resp.Result == nil && resp.Error == nil {
			continue
		}
		c.mu.Lock()
		ch, ok := c.pending[resp.ID]
		if ok {
			delete(c.pending, resp.ID)
		}
		c.mu.Unlock()
		if ok {
			select {
			case ch <- resp:
			default:
			}
		}
	}
	// on exit, fail all pending
	c.mu.Lock()
	for _, ch := range c.pending {
		select {
		case ch <- rpcResponse{Error: &rpcError{Code: -1, Message: "mcp server closed"}}:
		default:
		}
	}
	c.pending = make(map[int]chan rpcResponse)
	c.mu.Unlock()
}

func (c *Client) call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan rpcResponse, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	// write with mutex to avoid interleaving
	c.mu.Lock()
	_, err = c.stdin.Write(append(data, '\n'))
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("mcp timeout for %s", method)
	}
}

func (c *Client) initialize(ctx context.Context) error {
	params := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]interface{}{
			"name":    "pi-go",
			"version": "0.1.0",
		},
	}
	_, err := c.call(ctx, "initialize", params)
	if err != nil {
		return err
	}
	// send initialized notification (no id, no response expected)
	notify := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}
	data, _ := json.Marshal(notify)
	c.mu.Lock()
	_, _ = c.stdin.Write(append(data, '\n'))
	c.mu.Unlock()
	// small delay for server to be ready
	time.Sleep(100 * time.Millisecond)
	return nil
}

func (c *Client) discoverTools(ctx context.Context) error {
	result, err := c.call(ctx, "tools/list", map[string]interface{}{})
	if err != nil {
		return err
	}
	var resp struct {
		Tools []struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			InputSchema map[string]interface{} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return err
	}
	for _, t := range resp.Tools {
		schema := convertSchema(t.InputSchema)
		tool := &MCPTool{
			client:      c,
			serverName:  c.Name,
			toolName:    t.Name,
			description: t.Description,
			schema:      schema,
		}
		c.tools = append(c.tools, tool)
	}
	return nil
}

func convertSchema(in map[string]interface{}) types.ToolParameterSchema {
	var out types.ToolParameterSchema
	out.Type = "object"
	out.Properties = make(map[string]types.PropertyDef)
	if in == nil {
		return out
	}
	data, _ := json.Marshal(in)
	var raw map[string]interface{}
	if json.Unmarshal(data, &raw) != nil {
		return out
	}
	if t, ok := raw["type"].(string); ok && t != "" {
		out.Type = t
	}
	if props, ok := raw["properties"].(map[string]interface{}); ok {
		for k, v := range props {
			if vm, ok := v.(map[string]interface{}); ok {
				pd := types.PropertyDef{}
				if tt, ok := vm["type"].(string); ok {
					pd.Type = tt
				} else {
					pd.Type = "string"
				}
				if d, ok := vm["description"].(string); ok {
					pd.Description = d
				}
				if enum, ok := vm["enum"].([]interface{}); ok {
					for _, e := range enum {
						if s, ok := e.(string); ok {
							pd.Enum = append(pd.Enum, s)
						}
					}
				}
				out.Properties[k] = pd
			}
		}
	}
	if req, ok := raw["required"].([]interface{}); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				out.Required = append(out.Required, s)
			}
		}
	}
	if desc, ok := raw["description"].(string); ok {
		out.Description = desc
	}
	return out
}

// CallTool invokes a tool on the server
func (c *Client) CallTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	params := map[string]interface{}{
		"name":      toolName,
		"arguments": args,
	}
	result, err := c.call(ctx, "tools/call", params)
	if err != nil {
		return nil, err
	}
	var out interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		return string(result), nil
	}
	return out, nil
}

func (c *Client) Tools() []*MCPTool {
	return c.tools
}

func (c *Client) Close() error {
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
	if c.readerDone != nil {
		select {
		case <-c.readerDone:
		case <-time.After(2 * time.Second):
		}
	}
	return nil
}

// Manager manages multiple MCP servers
type Manager struct {
	Clients map[string]*Client
}

func NewManager() *Manager {
	return &Manager{Clients: make(map[string]*Client)}
}

func (m *Manager) LoadAndStart(ctx context.Context) (int, []string, error) {
	cfg, cfgPath, err := LoadConfig()
	if err != nil {
		return 0, nil, err
	}
	if len(cfg.MCPServers) == 0 {
		return 0, nil, nil
	}
	var started int
	var errs []string
	for name, scfg := range cfg.MCPServers {
		if scfg.Disabled {
			continue
		}
		client := NewClient(name, scfg)
		if err := client.Start(ctx); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v (config: %s)", name, err, cfgPath))
			continue
		}
		m.Clients[name] = client
		started++
	}
	return started, errs, nil
}

func (m *Manager) AllTools() []*MCPTool {
	var all []*MCPTool
	for _, c := range m.Clients {
		all = append(all, c.Tools()...)
	}
	return all
}

func (m *Manager) Close() {
	for _, c := range m.Clients {
		_ = c.Close()
	}
}

func (m *Manager) Status() string {
	if len(m.Clients) == 0 {
		return "No MCP servers connected"
	}
	var s string
	for name, c := range m.Clients {
		s += fmt.Sprintf("- %s: %d tools (%s %v)\n", name, len(c.Tools()), c.Config.Command, c.Config.Args)
	}
	return s
}
