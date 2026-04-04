package commands

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spencer-weaver/gollama/internal/agent"
	"github.com/spencer-weaver/gollama/internal/config"
	gcontext "github.com/spencer-weaver/gollama/internal/context"
	"github.com/spencer-weaver/gollama/internal/history"
	"github.com/spencer-weaver/gollama/internal/llm"
)

func Chat(args []string) error {
	fs := flag.NewFlagSet("chat", flag.ContinueOnError)
	projectDir := fs.String("project", "", "path to project directory")
	fresh := fs.Bool("fresh", false, "start a new session, discard existing history")
	sessionName := fs.String("session", "default", "named session")
	modelOverride := fs.String("model", "", "override model")
	endpointOverride := fs.String("endpoint", "", "override endpoint")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Load config.
	cfg, err := config.LoadGollamaConfig()
	if err != nil {
		return fmt.Errorf("chat: load config: %w", err)
	}

	// Apply overrides.
	if *modelOverride != "" {
		cfg.Model = *modelOverride
	}
	if *endpointOverride != "" {
		cfg.Endpoint = *endpointOverride
	}

	// Resolve API key.
	apiKey := ""
	if cfg.APIKeyEnv != "" {
		apiKey = os.Getenv(cfg.APIKeyEnv)
	}

	// Resolve gobin path.
	gobinPath, err := config.ExpandHome(cfg.GobinPath)
	if err != nil {
		gobinPath = cfg.GobinPath
	}

	// Resolve project directory.
	if *projectDir == "" {
		*projectDir, _ = os.Getwd()
	}
	absProject, err := filepath.Abs(*projectDir)
	if err != nil {
		absProject = *projectDir
	}

	// Build components.
	client := llm.NewClient(cfg.Endpoint, cfg.Model, apiKey, cfg.MaxTokens)

	hist := history.NewHistory(*sessionName)
	if *fresh {
		hist.Clear()
	} else {
		if err := hist.Load(); err != nil {
			return fmt.Errorf("chat: load history: %w", err)
		}
		if cfg.MaxHistoryTurns > 0 {
			hist.Trim(cfg.MaxHistoryTurns)
		}
	}

	proj := gcontext.NewProjectContext(absProject)
	if !proj.Exists() {
		fmt.Printf("No README.md found in %s. Start by describing your project.\n", absProject)
	}

	a := agent.NewAgent(client, hist, proj, gobinPath, cfg.SystemPromptBase)

	// Startup banner.
	fmt.Printf("gollama v0.1.0 — %s — session: %s\n", cfg.Model, *sessionName)
	if absProject != "" {
		fmt.Printf("project: %s\n", absProject)
	}

	// REPL loop.
	scanner := bufio.NewScanner(os.Stdin)
	ctx := context.Background()
	for {
		fmt.Print("\ngollama > ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "exit" || input == "quit" {
			break
		}
		_, err := a.Run(ctx, input)
		fmt.Println()
		if err != nil {
			fmt.Printf("error: %v\n", err)
		}
	}
	return nil
}

func resolveConfigPath() string {
	exe, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "..", "..", "config", "gollama.json")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return filepath.Join("config", "gollama.json")
}
