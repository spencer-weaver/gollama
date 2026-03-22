package commands

import (
	"flag"
	"fmt"
	"strings"

	"github.com/spencer-weaver/gollama/chat"
	"github.com/spencer-weaver/gollama/internal/config"
	"github.com/spencer-weaver/gollama/internal/tools"
)

// ChatHandler is the CLI entry point for the "chat" command.
//
// Flags:
//
//	-m <name>        load a model config from models/<name>.json
//	--tools <name>   enable a tool set without loading a model config (e.g. research, analyze)
//	--no-history     disable conversation history for this call
func ChatHandler(args []string) error {
	flags := flag.NewFlagSet("chat", flag.ContinueOnError)
	modelName := flags.String("m", "", "model config name (from models/)")
	modeFlag := flags.String("tools", "", "tool set to enable (e.g. research, analyze)")
	noHistory := flags.Bool("no-history", false, "disable conversation history")
	showThinking := flags.Bool("show-thinking", false, "show model reasoning (<think> blocks) in output")
	if err := flags.Parse(args); err != nil {
		return err
	}

	remaining := flags.Args()
	if len(remaining) == 0 {
		return fmt.Errorf("no user prompt supplied")
	}
	userPrompt := strings.Join(remaining, " ")

	cfg := config.GetGlobal()

	// Resolve models directory.
	modelsDir := cfg.ModelsDir
	if modelsDir == "" {
		modelsDir = "models"
	}

	// Load model config if -m was provided.
	var mc *config.ModelConfig
	if *modelName != "" {
		var err error
		mc, err = config.LoadModelConfig(modelsDir, *modelName)
		if err != nil {
			return err
		}
	}

	// Build chat config; model config fields override global where set.
	chatCfg := chat.Config{
		Host:         cfg.Host,
		Port:         cfg.Port,
		Model:        cfg.Model,
		System:       cfg.System,
		Temperature:  cfg.Temperature,
		MaxTokens:    cfg.MaxTokens,
		ShowThinking: *showThinking,
	}
	if mc != nil {
		if mc.Model != "" {
			chatCfg.Model = mc.Model
		}
		if mc.System != "" {
			chatCfg.System = mc.System
		}
		if mc.Temperature != 0 {
			chatCfg.Temperature = mc.Temperature
		}
		if mc.MaxTokens != 0 {
			chatCfg.MaxTokens = mc.MaxTokens
		}
	}

	// Determine active tools. Priority: model config tools > --mode flag > model config toolMode.
	toolMode := *modeFlag
	if toolMode == "" && mc != nil {
		toolMode = mc.ToolMode
	}

	var allowedTools []string
	if mc != nil && len(mc.Tools) > 0 {
		allowedTools = mc.Tools
	} else if toolMode != "" {
		allowedTools = tools.ToolsForMode(toolMode)
		if allowedTools == nil {
			return fmt.Errorf("unknown tool mode %q", toolMode)
		}
	}

	// Prepend tool guide to system prompt when tools are active.
	// Prepending ensures the model sees the tool instructions before its
	// persona/task description, which improves compliance on smaller models.
	reg := tools.DefaultRegistry()
	if len(allowedTools) > 0 {
		chatCfg.System = reg.ToolGuide(allowedTools) + chatCfg.System
	}

	// Build conversation history.
	useHistory := cfg.History && !*noHistory
	var history []chat.Msg
	if useHistory {
		for _, m := range cfg.HistoryMessages {
			history = append(history, chat.Msg{Role: m.Role, Content: m.Content})
		}
	}

	// Tool loop: run up to maxIter turns so the model can chain tool calls.
	const maxIter = 10
	currentPrompt := userPrompt
	var finalReply string

	for i := 0; i < maxIter; i++ {
		reply, err := chat.ChatWithHistory(chatCfg, history, currentPrompt)
		if err != nil {
			return err
		}

		// If no tools are active, or the model produced no tool calls, we're done.
		if len(allowedTools) == 0 {
			finalReply = reply
			break
		}

		calls, cleaned, hasCalls := tools.ParseToolCalls(reply)
		if !hasCalls {
			finalReply = reply
			break
		}

		// Print any text the model wrote before the tool_calls block.
		if cleaned != "" {
			fmt.Println(cleaned)
		}

		// Show which tools are being called.
		names := make([]string, len(calls))
		for j, c := range calls {
			names[j] = c.Tool
		}
		fmt.Printf("[calling tools: %s]\n", strings.Join(names, ", "))

		// Execute all tool calls and build results message.
		toolResults := reg.RunToolLoop(calls)

		// Extend history with this turn so the model has full context next round.
		history = append(history,
			chat.Msg{Role: "user", Content: currentPrompt},
			chat.Msg{Role: "assistant", Content: reply},
		)
		currentPrompt = toolResults

		// On final iteration, surface tool results as the reply.
		if i == maxIter-1 {
			finalReply = toolResults
		}
	}

	fmt.Println(finalReply)

	// Persist the original user prompt + final reply to history.
	if useHistory {
		cfg.HistoryMessages = append(cfg.HistoryMessages,
			config.Msg{Role: "user", Content: userPrompt},
			config.Msg{Role: "assistant", Content: finalReply},
		)
		if err := config.SaveConfigFile(config.GetGlobalPath(), cfg); err != nil {
			return fmt.Errorf("failed to persist history: %w", err)
		}
	}

	return nil
}
