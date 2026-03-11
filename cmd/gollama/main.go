package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/spencer-weaver/gollama/internal/commands"
	"github.com/spencer-weaver/gollama/internal/config"
)

func usage() {
	fmt.Println("usage: gollama <command> [args...]")
	fmt.Println("commands:")
	fmt.Println("  chat      Send a chat message to Ollama")
	fmt.Println("  ...")
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	flags := flag.NewFlagSet("gollama", flag.ContinueOnError)
	var configPath string
	flags.StringVar(&configPath, "c", "config/gollama.json", "path to config file")
	if err := flags.Parse(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "flag parse error: %v\n", err)
		os.Exit(1)
	}

	if configPath == "" {
		configPath = "config/gollama.json"
	}

	config.SetGlobalPath(configPath)

	// Load config (you can make the path a flag if you want)
	cfg := &config.GollamaConfig{}
	if err := config.LoadConfigData(config.GetGlobalPath(), cfg); err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	// Store the config globally so commands can access it
	config.SetGlobal(cfg)

	switch os.Args[1] {
	case "chat":
		if err := commands.ChatHandler(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(1)
	}
}
