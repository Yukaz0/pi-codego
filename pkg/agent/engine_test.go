package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"pi/pkg/session"
	"pi/pkg/tools"
	"pi/pkg/types"
)

// --- test doubles -----------------------------------------------------------

// mockProvider plays back a scripted stream per turn and records requests.
type mockProvider struct {
	mu       sync.Mutex
	streams  []func(ctx context.Context) <-chan types.StreamEvent
	calls    int
	requests []types.CompletionRequest
}

func (m *mockProvider) Name() string { return "mock" }

func (m *mockProvider) Stream(ctx context.Context, req types.CompletionRequest) (<-chan types.StreamEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.calls >= len(m.streams) {
		return nil, fmt.Errorf("mockProvider: no scripted stream for call %d", m.calls)
	}
	i := m.calls
	m.calls++
	m.requests = append(m.requests, req)
	return m.streams[i](ctx), nil
}

type fakeTool struct {
	name string
	exec func(ctx context.Context, argsJSON string) (string, error)
}

func (f *fakeTool) Name() string        { return f.name }
func (f *fakeTool) Description() string { return "test tool" }
func (f *fakeTool) Definition() types.ToolDefinition {
	return types.ToolDefinition{Name: f.name}
}
func (f *fakeTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	return f.exec(ctx, argsJSON)
}

func contentStream(text string) func(context.Context) <-chan types.StreamEvent {
	return func(context.Context) <-chan types.StreamEvent {
		ch := make(chan types.StreamEvent, 2)
		ch <- types.StreamEvent{Type: types.EventContentDelta, ContentDelta: text}
		ch <- types.StreamEvent{Type: types.EventDone}
		close(ch)
		return ch
	}
}

func toolCallStream(calls ...types.ToolCall) func(context.Context) <-chan types.StreamEvent {
	return func(context.Context) <-chan types.StreamEvent {
		ch := make(chan types.StreamEvent, len(calls)+2)
		ch <- types.StreamEvent{Type: types.EventContentDelta, ContentDelta: "using tools"}
		for i := range calls {
			ch <- types.StreamEvent{Type: types.EventToolCallDone, ToolCall: &calls[i]}
		}
		ch <- types.StreamEvent{Type: types.EventDone}
		close(ch)
		return ch
	}
}

func newEngineWith(t *testing.T, mp types.Provider, reg *tools.Registry) (*Engine, *session.Tree) {
	t.Helper()
	tree := session.NewTree()
	eng := NewEngine(EngineConfig{
		Provider:    mp,
		Model:       "mock-model",
		Tools:       reg,
		SessionTree: tree,
	})
	return eng, tree
}

// --- tests ------------------------------------------------------------------

// Regression test: a steering interrupt between tool calls must still emit a
// tool_result for every tool_call. Dangling tool_calls cause OpenAI/Anthropic
// to reject the next request.
func TestSteeringInterruptNeverLeavesDanglingToolCalls(t *testing.T) {
	mp := &mockProvider{
		streams: []func(context.Context) <-chan types.StreamEvent{
			toolCallStream(
				types.ToolCall{ID: "call-1", Type: "function",
					Function: types.FunctionCall{Name: "steer_tool", Arguments: "{}"}},
				types.ToolCall{ID: "call-2", Type: "function",
					Function: types.FunctionCall{Name: "steer_tool", Arguments: "{}"}},
			),
			contentStream("redirected"),
		},
	}

	var (
		eng  *Engine
		tree *session.Tree
	)
	steerFired := false
	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "steer_tool", exec: func(_ context.Context, _ string) (string, error) {
		// The tool fires its own steering interrupt mid-turn: the engine
		// must then skip call-2 and synthesize a tool_result for it.
		if !steerFired {
			eng.Steering.Steer("change topic")
			steerFired = true
		}
		return "ok", nil
	}})

	eng, tree = newEngineWith(t, mp, reg)

	if err := eng.RunTurn(context.Background(), "run the tool twice"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	// Collect every tool_call id and every tool_result id from history.
	hist := tree.GetLinearHistory("")
	calledIDs := map[string]bool{}
	resultFor := map[string]types.ToolResult{}
	for _, msg := range hist {
		for _, tc := range msg.ToolCalls {
			calledIDs[tc.ID] = true
		}
		for _, tr := range msg.ToolResults {
			resultFor[tr.ToolCallID] = tr
		}
	}

	for id := range calledIDs {
		tr, ok := resultFor[id]
		if !ok {
			t.Errorf("DANGLING tool_call %q has no matching tool_result", id)
			continue
		}
		switch id {
		case "call-1":
			if tr.Content != "ok" || tr.IsError {
				t.Errorf("call-1 result = %+v, want 'ok'", tr)
			}
		case "call-2":
			if !strings.Contains(tr.Content, "interrupted") || tr.IsError {
				t.Errorf("call-2 result = %+v, want interrupted notice", tr)
			}
		}
	}

	// The turn must continue after the interrupt and finish cleanly.
	if mp.calls != 2 {
		t.Errorf("provider calls = %d, want 2 (continue after steering)", mp.calls)
	}
	last := hist[len(hist)-1]
	if last.Role != types.RoleAssistant || last.Content != "redirected" {
		t.Errorf("final message = %+v, want assistant 'redirected'", last)
	}
}

func TestFollowUpQueueDrainedIteratively(t *testing.T) {
	mp := &mockProvider{
		streams: []func(context.Context) <-chan types.StreamEvent{
			contentStream("first answer"),
			contentStream("second answer"),
		},
	}
	eng, tree := newEngineWith(t, mp, tools.DefaultRegistry())
	eng.Steering.QueueFollowUp("follow-up prompt")

	if err := eng.RunTurn(context.Background(), "initial prompt"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	if mp.calls != 2 {
		t.Fatalf("provider calls = %d, want 2", mp.calls)
	}
	hist := tree.GetLinearHistory("")
	userMsgs := []string{}
	for _, m := range hist {
		if m.Role == types.RoleUser {
			userMsgs = append(userMsgs, m.Content)
		}
	}
	if len(userMsgs) != 2 ||
		userMsgs[0] != "initial prompt" ||
		userMsgs[1] != "follow-up prompt" {
		t.Errorf("user messages = %#v", userMsgs)
	}
}

func TestStreamErrorAbortsTurnWithoutTools(t *testing.T) {
	boom := errors.New("boom: upstream failure")
	mp := &mockProvider{
		streams: []func(context.Context) <-chan types.StreamEvent{
			func(context.Context) <-chan types.StreamEvent {
				ch := make(chan types.StreamEvent, 3)
				ch <- types.StreamEvent{Type: types.EventContentDelta, ContentDelta: "partial"}
				ch <- types.StreamEvent{Type: types.EventError, Error: boom}
				ch <- types.StreamEvent{Type: types.EventDone}
				close(ch)
				return ch
			},
		},
	}
	eng, tree := newEngineWith(t, mp, tools.NewRegistry())

	err := eng.RunTurn(context.Background(), "hi")
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want upstream boom error", err)
	}

	// Partial output stays in the tree for debugging...
	hist := tree.GetLinearHistory("")
	found := false
	for _, m := range hist {
		if m.Role == types.RoleAssistant && strings.Contains(m.Content, "partial") {
			found = true
		}
	}
	if !found {
		t.Error("partial assistant output should remain in session tree")
	}
}

func TestSimpleTurnNoTools(t *testing.T) {
	mp := &mockProvider{streams: []func(context.Context) <-chan types.StreamEvent{
		contentStream("halo dunia"),
	}}
	eng, tree := newEngineWith(t, mp, tools.NewRegistry())

	if err := eng.RunTurn(context.Background(), "hello"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if mp.calls != 1 {
		t.Errorf("provider calls = %d, want exactly 1 (no follow-up loop)", mp.calls)
	}
	hist := tree.GetLinearHistory("")
	if len(hist) != 2 {
		t.Fatalf("history = %d messages, want 2", len(hist))
	}
	if hist[1].Role != types.RoleAssistant || hist[1].Content != "halo dunia" {
		t.Errorf("assistant message = %+v", hist[1])
	}
}
