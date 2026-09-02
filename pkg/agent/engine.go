package agent

import (
	"context"
	"fmt"
	"strings"

	piContext "pi/pkg/context"
	"pi/pkg/session"
	"pi/pkg/tools"
	"pi/pkg/types"
)

type EventListener interface {
	OnTurnStart()
	OnTurnEnd()
	OnContentDelta(delta string)
	OnThinkingDelta(delta string)
	OnToolExecutionStart(toolName string, argsJSON string)
	OnToolExecutionEnd(toolName string, result string, isError bool)
	OnUsage(usage types.TokenUsage)
	OnError(err error)
}

type DefaultEventListener struct{}

func (d *DefaultEventListener) OnTurnStart()                                                  {}
func (d *DefaultEventListener) OnTurnEnd()                                                    {}
func (d *DefaultEventListener) OnContentDelta(delta string)                                   {}
func (d *DefaultEventListener) OnThinkingDelta(delta string)                                  {}
func (d *DefaultEventListener) OnToolExecutionStart(toolName string, argsJSON string)         {}
func (d *DefaultEventListener) OnToolExecutionEnd(toolName string, result string, isErr bool) {}
func (d *DefaultEventListener) OnUsage(usage types.TokenUsage)                                {}
func (d *DefaultEventListener) OnError(err error)                                             {}

type EngineConfig struct {
	Provider          types.Provider
	Model             string
	Tools             *tools.Registry
	SessionTree       *session.Tree
	InstructionLoader *piContext.InstructionLoader
	SkillLoader       *piContext.SkillLoader
	PromptLoader      *piContext.PromptLoader
	Compactor         *session.Compactor
	Listener          EventListener
	// ThinkingLevel is forwarded to the provider on every request
	// ("off", "minimal", "low", "medium", "high", "xhigh", "max").
	ThinkingLevel string
}

type Engine struct {
	Config   EngineConfig
	Steering *SteeringController

	// PendingImages are attached to the next user message once, then cleared.
	// Used by the TUI for clipboard image paste (Ctrl+V).
	PendingImages []types.ImageAttachment
}

func NewEngine(cfg EngineConfig) *Engine {
	if cfg.Tools == nil {
		cfg.Tools = tools.DefaultRegistry()
	}
	if cfg.SessionTree == nil {
		cfg.SessionTree = session.NewTree()
	}
	if cfg.Listener == nil {
		cfg.Listener = &DefaultEventListener{}
	}
	return &Engine{
		Config:   cfg,
		Steering: NewSteeringController(),
	}
}

func (e *Engine) BuildSystemPrompt() string {
	basePrompt := piContext.DefaultSystemPrompt()

	if e.Config.InstructionLoader != nil {
		sysOverride, rules, _ := e.Config.InstructionLoader.LoadInstructions()
		if sysOverride != "" {
			basePrompt = sysOverride
		}
		if rules != "" {
			basePrompt += "\n\n" + rules
		}
	}

	if e.Config.SkillLoader != nil {
		skills, _ := e.Config.SkillLoader.LoadAvailableSkills()
		if len(skills) > 0 {
			basePrompt += piContext.FormatSkillsPrompt(skills)
		}
	}

	return basePrompt
}

// RunTurn executes a full turn, then drains any queued follow-up prompts
// iteratively (no recursion, so arbitrarily long queues are safe).
func (e *Engine) RunTurn(parentCtx context.Context, userPrompt string) error {
	e.Config.Listener.OnTurnStart()
	defer e.Config.Listener.OnTurnEnd()

	prompt := userPrompt
	for {
		if err := e.runSingleTurn(parentCtx, prompt); err != nil {
			return err
		}
		followUp, ok := e.Steering.PopFollowUp()
		if !ok {
			return nil
		}
		prompt = followUp
	}
}

func (e *Engine) runSingleTurn(parentCtx context.Context, userPrompt string) error {
	// 1. Add user message to session tree
	userMsg := types.NewUserMessage(userPrompt)
	if len(e.PendingImages) > 0 {
		userMsg.Images = e.PendingImages
		e.PendingImages = nil
	}
	e.Config.SessionTree.AddMessage(userMsg)

	systemPrompt := e.BuildSystemPrompt()

	for {
		select {
		case <-parentCtx.Done():
			return parentCtx.Err()
		default:
		}

		// Check if steering prompt was injected
		if steerPrompt, ok := e.Steering.PopSteeringPrompt(); ok {
			steerMsg := types.NewUserMessage("[Steering Interruption]: " + steerPrompt)
			e.Config.SessionTree.AddMessage(steerMsg)
		}

		// 2. Get history and apply compaction if needed
		linearHistory := e.Config.SessionTree.GetLinearHistory("")
		if e.Config.Compactor != nil {
			compacted, _, err := e.Config.Compactor.CompactIfNeeded(parentCtx, linearHistory)
			if err == nil {
				linearHistory = compacted
			}
		}

		// 3. Prepare completion request
		req := types.CompletionRequest{
			Model:         e.Config.Model,
			SystemPrompt:  systemPrompt,
			Messages:      linearHistory,
			Tools:         e.Config.Tools.Definitions(),
			ThinkingLevel: e.Config.ThinkingLevel,
		}

		turnCtx, cancelTurn := context.WithCancel(parentCtx)
		e.Steering.SetCancelFunc(cancelTurn)

		events, err := e.Config.Provider.Stream(turnCtx, req)
		if err != nil {
			cancelTurn()
			e.Config.Listener.OnError(err)
			return err
		}

		var fullAssistantContent strings.Builder
		var fullReasoning strings.Builder
		var toolCalls []types.ToolCall
		var streamErr error

		for ev := range events {
			switch ev.Type {
			case types.EventContentDelta:
				fullAssistantContent.WriteString(ev.ContentDelta)
				e.Config.Listener.OnContentDelta(ev.ContentDelta)
			case types.EventThinkingDelta:
				if ev.ThinkingDelta != "" {
					fullReasoning.WriteString(ev.ThinkingDelta)
					e.Config.Listener.OnThinkingDelta(ev.ThinkingDelta)
				}
			case types.EventToolCallDone:
				if ev.ToolCall != nil {
					toolCalls = append(toolCalls, *ev.ToolCall)
				}
			case types.EventError:
				if ev.Error != nil && ev.Error != context.Canceled {
					e.Config.Listener.OnError(ev.Error)
					streamErr = ev.Error
				}
			case types.EventDone:
				if ev.Usage != nil && e.Config.Listener != nil {
					e.Config.Listener.OnUsage(*ev.Usage)
				}
			}
		}
		cancelTurn()

		// Add Assistant message to session tree
		assistantMsg := types.NewAssistantMessageWithReasoning(fullAssistantContent.String(), fullReasoning.String(), toolCalls)
		e.Config.SessionTree.AddMessage(assistantMsg)

		// 4. If no tools were called, the turn is finished.
		// On a stream error with no tool calls, abort instead of silently
		// treating partial output as a completed turn.
		if len(toolCalls) == 0 {
			if streamErr != nil {
				return streamErr
			}
			break
		}

		// 5. Execute Tool Calls
		for i, tc := range toolCalls {
			// Check steering interrupt before executing next tool.
			// Remaining tool calls MUST still receive result messages,
			// otherwise the history contains dangling tool_calls and the
			// next API request will be rejected (OpenAI/Anthropic validate
			// that every tool_call has a matching tool_result).
			if steerPrompt, ok := e.Steering.PopSteeringPrompt(); ok {
				steerMsg := types.NewUserMessage("[Steering Interruption]: " + steerPrompt)
				e.Config.SessionTree.AddMessage(steerMsg)
				for _, pending := range toolCalls[i:] {
					result := "[Tool execution skipped: interrupted by user steering]"
					e.Config.Listener.OnToolExecutionEnd(pending.Function.Name, result, false)
					e.Config.SessionTree.AddMessage(types.NewToolResultMessage(pending.ID, result, false))
				}
				break
			}

			e.Config.Listener.OnToolExecutionStart(tc.Function.Name, tc.Function.Arguments)
			result, toolErr := e.Config.Tools.Execute(parentCtx, tc.Function.Name, tc.Function.Arguments)

			isError := toolErr != nil
			if isError {
				result = fmt.Sprintf("Error: %v", toolErr)
			}

			e.Config.Listener.OnToolExecutionEnd(tc.Function.Name, result, isError)

			toolResultMsg := types.NewToolResultMessage(tc.ID, result, isError)
			e.Config.SessionTree.AddMessage(toolResultMsg)
		}
	}

	return nil
}
