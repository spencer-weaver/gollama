package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spencer-weaver/gollama/internal/agent"
	"github.com/spencer-weaver/gollama/internal/commands"
	"github.com/spencer-weaver/gollama/internal/config"
	"github.com/spencer-weaver/gollama/internal/conversation"
	"github.com/spencer-weaver/gollama/internal/handler"
	"github.com/spencer-weaver/gollama/internal/server"
	"github.com/spencer-weaver/gollama/internal/tasks"
)

func usage() {
	fmt.Println("usage: gollama [-c config] [command [flags] [args...]]")
	fmt.Println()
	fmt.Println("  Run with no command to start the HTTP conversation agent server.")
	fmt.Println()
	fmt.Println("global flags:")
	fmt.Println("  -c <path>   path to config file (optional; defaults resolved from binary location)")
	fmt.Println()
	fmt.Println("commands:")
	fmt.Println("  chat        [-m model] [--tools toolset] [--no-history] <message...>")
	fmt.Println("  brainstorm  [--threshold N] [--session path] [--sessions dir] <topic>")
	fmt.Println("  plan        [--session path] [--sessions dir] [--topic slug]")
	fmt.Println("  voice       [--topic topic] [--threshold N] [--stt backend] [--tts backend]")
	fmt.Println("  agent       [--audio-device dev] [--playback-device dev] [--stt-model model] [--proactive-delay N]")
	fmt.Println("  pcb-design  <name> [--threshold N] [--output path] [--session path]")
}

func main() {
	root := resolveRoot()
	config.SetGlobalRoot(root)

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

	cfg := config.DefaultConfig()
	if _, err := os.Stat(configPath); err == nil {
		if err := config.LoadConfigData(configPath, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "load config: %v\n", err)
			os.Exit(1)
		}
	} else if configExplicit {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	if cfg.ModelsDir == "" {
		cfg.ModelsDir = filepath.Join(root, "models")
	}
	cfg.ApplyEnv()
	config.SetGlobal(cfg)

	subArgs := flags.Args()

	// No subcommand → start the HTTP conversation agent server.
	if len(subArgs) == 0 {
		runServer(cfg)
		return
	}

	runCLI(subArgs)
}

func runServer(cfg *config.GollamaConfig) {
	store := conversation.NewStore()
	tm := tasks.NewManager()
	ag := agent.New(cfg, store, tm)

	addr := cfg.ListenAddr
	if addr == "" {
		addr = ":8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/conversation/process", handler.NewProcessHandler(ag))

	srv := server.New(addr, mux)
	log.Printf("gollama listening on %s", addr)
	log.Fatal(srv.ListenAndServe())
}

func runCLI(subArgs []string) {
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
	case "agent":
		if err := commands.AgentRun(subArgs[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "pcb-design":
		if err := commands.PCBDesignHandler(subArgs[1:]); err != nil {
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
