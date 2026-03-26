// Package agent implements the core conversation processing logic for the
// Home Assistant conversation agent API. It is decoupled from HTTP concerns.
package agent

import (
	"fmt"
	"log"
	"strings"

	"github.com/spencer-weaver/gollama/chat"
	"github.com/spencer-weaver/gollama/internal/config"
	"github.com/spencer-weaver/gollama/internal/conversation"
	"github.com/spencer-weaver/gollama/internal/tasks"
	"github.com/spencer-weaver/gollama/internal/tools"
)

const (
	maxToolIterations  = 10
	maxHistoryMessages = 40
)

// Agent processes conversation turns against an Ollama model.
type Agent struct {
	ollCfg  chat.Config
	store   *conversation.Store
	tasks   *tasks.Manager
	reg     *tools.Registry
	allowed []string // tool names available to the model
}

// New creates an Agent wired to the given config, store, and task manager.
// If cfg.AgentModel names a model config file it is loaded and applied on top
// of the global config values.
func New(cfg *config.GollamaConfig, store *conversation.Store, tm *tasks.Manager) *Agent {
	reg := tools.DefaultRegistry()
	// Re-register config-aware tools with explicit values from GollamaConfig.
	reg.Register(tools.NewSearchWebTool(cfg.SearXNGURL))
	reg.Register(tools.NewSpawnBackgroundTool(tm))
	reg.Register(tools.NewHAServiceTool(cfg.HAHost, cfg.HAToken))
	reg.Register(tools.NewHAStatesTool(cfg.HAHost, cfg.HAToken))

	ollCfg := chat.Config{
		Host:        cfg.Host,
		Port:        cfg.Port,
		Model:       cfg.Model,
		System:      cfg.System,
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxTokens,
	}

	// Default to the ha tool mode; override from model config below.
	allowed := tools.ToolsForMode("ha")

	if cfg.AgentModel != "" {
		mc, err := config.LoadModelConfig(cfg.ModelsDir, cfg.AgentModel)
		if err != nil {
			log.Printf("warning: could not load agent model %q: %v", cfg.AgentModel, err)
		} else {
			if mc.Model != "" {
				ollCfg.Model = mc.Model
			}
			if mc.System != "" {
				ollCfg.System = mc.System
			}
			if mc.Temperature != 0 {
				ollCfg.Temperature = mc.Temperature
			}
			if mc.MaxTokens != 0 {
				ollCfg.MaxTokens = mc.MaxTokens
			}
			if len(mc.Tools) > 0 {
				allowed = mc.Tools
			} else if mc.ToolMode != "" {
				allowed = tools.ToolsForMode(mc.ToolMode)
			}
		}
	}

	if allowed == nil {
		allowed = []string{}
	}

	return &Agent{
		ollCfg:  ollCfg,
		store:   store,
		tasks:   tm,
		reg:     reg,
		allowed: allowed,
	}
}

// ProcessTurn handles one voice command and returns the speech response.
func (a *Agent) ProcessTurn(convID, text string) (string, error) {
	history := a.store.Get(convID)
	if len(history) > maxHistoryMessages {
		history = history[len(history)-maxHistoryMessages:]
	}

	ollCfg := a.ollCfg
	if len(a.allowed) > 0 {
		ollCfg.System = a.reg.ToolGuide(a.allowed) + ollCfg.System
	}

	// If any background tasks finished since the last turn, fold their results
	// into the prompt so the model can report them naturally.
	prompt := text
	if results := a.tasks.DrainUpdates(); len(results) > 0 {
		var sb strings.Builder
		sb.WriteString("Background task results (report these to the user naturally):\n")
		for _, r := range results {
			if r.Err != nil {
				fmt.Fprintf(&sb, "- %s: failed: %v\n", r.Label, r.Err)
			} else {
				fmt.Fprintf(&sb, "- %s: %s\n", r.Label, r.Output)
			}
		}
		sb.WriteString("\nUser said: ")
		sb.WriteString(text)
		prompt = sb.String()
	}

	reply, err := a.runWithTools(ollCfg, history, prompt)
	if err != nil {
		return "", err
	}

	// Persist the turn with the original user text (not the injected prompt).
	a.store.Append(convID,
		chat.Msg{Role: "user", Content: text},
		chat.Msg{Role: "assistant", Content: reply},
	)

	return reply, nil
}

// runWithTools runs the model with an iterative tool-call loop.
func (a *Agent) runWithTools(ollCfg chat.Config, history []chat.Msg, prompt string) (string, error) {
	curHistory := history
	curPrompt := prompt

	for i := 0; i < maxToolIterations; i++ {
		reply, err := chat.ChatWithHistory(ollCfg, curHistory, curPrompt)
		if err != nil {
			return "", err
		}

		if len(a.allowed) == 0 {
			return reply, nil
		}

		calls, cleaned, found := tools.ParseToolCalls(reply)
		if !found {
			return reply, nil
		}

		results := a.reg.RunToolLoop(calls)
		curHistory = append(curHistory,
			chat.Msg{Role: "user", Content: curPrompt},
			chat.Msg{Role: "assistant", Content: cleaned},
		)
		curPrompt = results
	}

	return "", fmt.Errorf("tool loop exceeded %d iterations without a final response", maxToolIterations)
}
