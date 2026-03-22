package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spencer-weaver/gollama/internal/commands"
	"github.com/spencer-weaver/gollama/internal/config"
)

func usage() {
	fmt.Println("usage: gollama [-c config] <command> [flags] [args...]")
	fmt.Println()
	fmt.Println("global flags:")
	fmt.Println("  -c <path>   path to config file (optional; defaults resolved from binary location)")
	fmt.Println()
	fmt.Println("commands:")
	fmt.Println("  chat        [-m model] [--tools toolset] [--no-history] <message...>")
	fmt.Println("  brainstorm  [--threshold N] [--session path] [--sessions dir] <topic>")
	fmt.Println("  plan        [--session path] [--sessions dir] [--topic slug]")
	fmt.Println("  voice       [--topic topic] [--threshold N] [--stt backend] [--tts backend]")
	fmt.Println()
	fmt.Println("chat flags:")
	fmt.Println("  -m <name>          load agent model config from models/<name>.json")
	fmt.Println("  --tools <toolset>  enable a tool set without loading a model config (e.g. research, analyze)")
	fmt.Println("  --no-history       disable conversation history for this call")
	fmt.Println()
	fmt.Println("brainstorm flags:")
	fmt.Println("  --threshold N      completeness score (0-100) to end brainstorm (default 80)")
	fmt.Println("  --session path     path to session file (default: <root>/sessions/<slug>.json)")
	fmt.Println("  --sessions dir     directory for session files")
	fmt.Println("  --no-plan          skip automatic plan prompt after brainstorm ends")
	fmt.Println()
	fmt.Println("plan flags:")
	fmt.Println("  --session path     path to session file")
	fmt.Println("  --sessions dir     directory for session files")
	fmt.Println("  --topic slug       derive session path from topic slug")
}

func main() {
	// Resolve the project root from the real binary location (follows symlinks).
	root := resolveRoot()
	config.SetGlobalRoot(root)

	// Parse global flags that appear before the subcommand.
	flags := flag.NewFlagSet("gollama", flag.ContinueOnError)
	flags.Usage = usage
	var configPath string
	var configExplicit bool
	flags.StringVar(&configPath, "c", "", "path to config file")

	if err := flags.Parse(os.Args[1:]); err != nil {
		os.Exit(1)
	}

	if configPath == "" {
		configPath = filepath.Join(root, "config", "gollama.json")
	} else {
		configExplicit = true
	}
	config.SetGlobalPath(configPath)

	subArgs := flags.Args()
	if len(subArgs) == 0 {
		usage()
		os.Exit(1)
	}

	// Load config file if it exists; otherwise use built-in defaults.
	cfg := config.DefaultConfig()
	if _, err := os.Stat(configPath); err == nil {
		if err := config.LoadConfigData(configPath, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "load config: %v\n", err)
			os.Exit(1)
		}
	} else if configExplicit {
		// The user explicitly passed -c; fail if it can't be read.
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	// If ModelsDir is not set, derive it from the project root.
	if cfg.ModelsDir == "" {
		cfg.ModelsDir = filepath.Join(root, "models")
	}

	config.SetGlobal(cfg)

	switch subArgs[0] {
	case "chat":
		if err := commands.ChatHandler(subArgs[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "brainstorm":
		if err := commands.BrainstormHandler(subArgs[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "plan":
		if err := commands.PlanHandler(subArgs[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "voice":
		if err := commands.VoiceHandler(subArgs[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(1)
	}
}

// resolveRoot returns the project root directory by resolving the real path of
// the running binary (following symlinks) and walking up one level from bin/.
func resolveRoot() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		real = exe
	}
	// binary is at <root>/bin/gollama → root is one level up
	return filepath.Dir(filepath.Dir(real))
}
