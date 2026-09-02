package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"pi/pkg/agent"
	"pi/pkg/provider"
	"pi/pkg/session"
	"pi/pkg/tools"
	"pi/pkg/types"
)

// Server implements JSONL over stdin/stdout RPC protocol.
// It is headless: each line on stdin is a JSON RPCRequest, each line on stdout is a JSON RPCEvent.
type Server struct {
	Engine  *agent.Engine
	Session *session.Tree
	Storage *session.Storage

	Model    string
	Provider string

	mu         sync.Mutex
	cancelTurn context.CancelFunc
	currentCtx context.Context
	writer     io.Writer
	writerMu   sync.Mutex
}

// ServerConfig holds deps for creating a new RPC server
type ServerConfig struct {
	Model    string
	Provider string
	BaseURL  string
	APIKey   string
	Session  *session.Tree
	Storage  *session.Storage
	Engine   *agent.Engine
	Writer   io.Writer // defaults to os.Stdout if nil
}

func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.Session == nil {
		cfg.Session = session.NewTree()
	}
	if cfg.Storage == nil {
		cfg.Storage = session.NewStorage("")
	}
	var eng *agent.Engine
	if cfg.Engine != nil {
		eng = cfg.Engine
	} else {
		// Build provider from model string if possible, fallback to mock/noop handled at prompt time
		eng = agent.NewEngine(agent.EngineConfig{
			SessionTree: cfg.Session,
			Tools:       tools.DefaultRegistry(),
		})
		// Try to resolve provider if model is given
		if cfg.Model != "" {
			pvd, modelName, err := provider.ResolveProvider(provider.Config{
				Model:    cfg.Model,
				Provider: cfg.Provider,
				BaseURL:  cfg.BaseURL,
				APIKey:   cfg.APIKey,
			})
			if err == nil {
				eng.Config.Provider = pvd
				eng.Config.Model = modelName
			} else {
				// keep engine with nil provider; error will be emitted on prompt
				eng.Config.Model = cfg.Model
			}
		}
	}

	return &Server{
		Engine:   eng,
		Session:  cfg.Session,
		Storage:  cfg.Storage,
		Model:    cfg.Model,
		Provider: cfg.Provider,
	}, nil
}

// rpcListener bridges agent.EventListener to RPC JSONL events
type rpcListener struct {
	emit func(RPCEvent)
}

func (r *rpcListener) OnTurnStart() { r.emit(RPCEvent{Type: EventTurnStart}) }
func (r *rpcListener) OnTurnEnd()   { r.emit(RPCEvent{Type: EventTurnEnd}) }
func (r *rpcListener) OnContentDelta(delta string) {
	r.emit(RPCEvent{Type: EventContentDelta, Delta: delta})
}
func (r *rpcListener) OnThinkingDelta(delta string) {
	r.emit(RPCEvent{Type: EventThinkingDelta, Delta: delta})
}
func (r *rpcListener) OnToolExecutionStart(toolName string, argsJSON string) {
	r.emit(RPCEvent{Type: EventToolStart, Tool: toolName, Args: argsJSON})
}
func (r *rpcListener) OnToolExecutionEnd(toolName string, result string, isError bool) {
	r.emit(RPCEvent{Type: EventToolEnd, Tool: toolName, Result: result, IsError: isError})
}
func (r *rpcListener) OnUsage(usage types.TokenUsage) {
	r.emit(RPCEvent{Type: EventDone, Usage: &Usage{
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
	}})
}
func (r *rpcListener) OnError(err error) {
	if err != nil {
		r.emit(RPCEvent{Type: EventError, Error: err.Error()})
	}
}

func (s *Server) emit(writer io.Writer, ev RPCEvent) {
	s.writerMu.Lock()
	defer s.writerMu.Unlock()
	b, _ := json.Marshal(ev)
	_, _ = writer.Write(append(b, '\n'))
}

// Run reads JSONL requests from reader and writes events to writer (or s.writer if configured).
// It blocks until reader hits EOF or context cancellation.
func (s *Server) Run(ctx context.Context, reader io.Reader, writer io.Writer) error {
	if writer == nil {
		writer = s.writer
	}
	if writer == nil {
		return fmt.Errorf("no writer configured")
	}
	// Use large scanner buffer (10MB) to handle large prompts
	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req RPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.emit(writer, RPCEvent{Type: EventError, Error: fmt.Sprintf("invalid request json: %v", err)})
			continue
		}
		if err := s.handleRequest(ctx, req, writer); err != nil {
			// handleRequest already emitted error event; continue loop
			_ = err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func (s *Server) handleRequest(parentCtx context.Context, req RPCRequest, writer io.Writer) error {
	switch req.Type {
	case RequestPrompt:
		return s.handlePrompt(parentCtx, req, writer)
	case RequestAbort:
		s.mu.Lock()
		if s.cancelTurn != nil {
			s.cancelTurn()
		}
		s.mu.Unlock()
		s.emit(writer, RPCEvent{Type: EventAborted})
		return nil
	case RequestSetModel:
		model := req.Model
		if model == "" {
			s.emit(writer, RPCEvent{Type: EventError, Error: "set_model requires 'model' field"})
			return fmt.Errorf("missing model")
		}
		pvd, modelName, err := provider.ResolveProvider(provider.Config{
			Model:    model,
			Provider: req.Provider,
			BaseURL:  req.BaseURL,
			APIKey:   req.APIKey,
		})
		if err != nil {
			s.emit(writer, RPCEvent{Type: EventError, Error: err.Error()})
			return err
		}
		s.mu.Lock()
		s.Engine.Config.Provider = pvd
		s.Engine.Config.Model = modelName
		s.Model = modelName
		s.Provider = req.Provider
		s.mu.Unlock()
		s.emit(writer, RPCEvent{Type: EventDone, Model: modelName})
		return nil
	case RequestNewSession:
		s.mu.Lock()
		newTree := session.NewTree()
		s.Session = newTree
		s.Engine.Config.SessionTree = newTree
		s.mu.Unlock()
		// Generate session id from random
		sessionID := fmt.Sprintf("sess-%s", newTree.CurrentLeafID)
		if sessionID == "sess-" {
			sessionID = "sess-new"
		}
		// Use generateID-like value: borrow from tree's next id? just use random hex via tree? simpler
		// We already have storage; emit
		s.emit(writer, RPCEvent{Type: EventSessionCreated, SessionID: sessionID})
		return nil
	case RequestResumeSession:
		if req.SessionID == "" {
			s.emit(writer, RPCEvent{Type: EventError, Error: "resume_session requires session_id"})
			return fmt.Errorf("missing session_id")
		}
		tree, err := s.Storage.LoadSession(req.SessionID)
		if err != nil {
			s.emit(writer, RPCEvent{Type: EventError, Error: fmt.Sprintf("failed to load session %s: %v", req.SessionID, err)})
			return err
		}
		s.mu.Lock()
		s.Session = tree
		s.Engine.Config.SessionTree = tree
		s.mu.Unlock()
		s.emit(writer, RPCEvent{Type: EventSessionCreated, SessionID: req.SessionID})
		return nil
	default:
		s.emit(writer, RPCEvent{Type: EventError, Error: fmt.Sprintf("unknown request type: %s", req.Type)})
		return fmt.Errorf("unknown type %s", req.Type)
	}
}

func (s *Server) handlePrompt(parentCtx context.Context, req RPCRequest, writer io.Writer) error {
	if req.Message == "" {
		s.emit(writer, RPCEvent{Type: EventError, Error: "prompt requires 'message' field"})
		return fmt.Errorf("empty prompt")
	}
	s.mu.Lock()
	if s.Engine.Config.Provider == nil {
		s.mu.Unlock()
		s.emit(writer, RPCEvent{Type: EventError, Error: "no provider configured. Use set_model first or start with --model"})
		return fmt.Errorf("no provider")
	}
	// Create cancellable context for this turn
	turnCtx, cancel := context.WithCancel(parentCtx)
	s.cancelTurn = cancel
	s.currentCtx = turnCtx
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.cancelTurn = nil
		s.currentCtx = nil
		s.mu.Unlock()
		cancel()
	}()

	emitFn := func(ev RPCEvent) { s.emit(writer, ev) }
	emitFn(RPCEvent{Type: EventStart, SessionID: s.Session.CurrentLeafID})

	// Wrap engine listener to emit RPC events
	origListener := s.Engine.Config.Listener
	rpcL := &rpcListener{emit: emitFn}
	// chain: set temporary listener
	s.mu.Lock()
	s.Engine.Config.Listener = &chainedListener{primary: rpcL, secondary: origListener}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.Engine.Config.Listener = origListener
		s.mu.Unlock()
	}()

	err := s.Engine.RunTurn(turnCtx, req.Message)
	if err != nil {
		if err == context.Canceled {
			s.emit(writer, RPCEvent{Type: EventAborted})
			return nil
		}
		s.emit(writer, RPCEvent{Type: EventError, Error: err.Error()})
		return err
	}
	s.emit(writer, RPCEvent{Type: EventDone})
	return nil
}

// chainedListener forwards to both rpc and original listener
type chainedListener struct {
	primary   agent.EventListener
	secondary agent.EventListener
}

func (c *chainedListener) OnTurnStart() {
	if c.primary != nil {
		c.primary.OnTurnStart()
	}
	if c.secondary != nil {
		c.secondary.OnTurnStart()
	}
}
func (c *chainedListener) OnTurnEnd() {
	if c.primary != nil {
		c.primary.OnTurnEnd()
	}
	if c.secondary != nil {
		c.secondary.OnTurnEnd()
	}
}
func (c *chainedListener) OnContentDelta(delta string) {
	if c.primary != nil {
		c.primary.OnContentDelta(delta)
	}
	if c.secondary != nil {
		c.secondary.OnContentDelta(delta)
	}
}
func (c *chainedListener) OnThinkingDelta(delta string) {
	if c.primary != nil {
		c.primary.OnThinkingDelta(delta)
	}
	if c.secondary != nil {
		c.secondary.OnThinkingDelta(delta)
	}
}
func (c *chainedListener) OnToolExecutionStart(tool string, args string) {
	if c.primary != nil {
		c.primary.OnToolExecutionStart(tool, args)
	}
	if c.secondary != nil {
		c.secondary.OnToolExecutionStart(tool, args)
	}
}
func (c *chainedListener) OnUsage(usage types.TokenUsage) {
	if c.primary != nil {
		c.primary.OnUsage(usage)
	}
	if c.secondary != nil {
		c.secondary.OnUsage(usage)
	}
}
func (c *chainedListener) OnToolExecutionEnd(tool string, result string, isError bool) {
	if c.primary != nil {
		c.primary.OnToolExecutionEnd(tool, result, isError)
	}
	if c.secondary != nil {
		c.secondary.OnToolExecutionEnd(tool, result, isError)
	}
}
func (c *chainedListener) OnError(err error) {
	if c.primary != nil {
		c.primary.OnError(err)
	}
	if c.secondary != nil {
		c.secondary.OnError(err)
	}
}
