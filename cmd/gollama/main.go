package main

import (
	"fmt"
	"os"

	"github.com/spencer-weaver/gollama/internal/commands"
	"github.com/spencer-weaver/gollama/internal/config"
)

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		if err := commands.Chat(args); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	switch args[0] {
	case "sessions":
		if err := commands.Sessions(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "config":
		cfg, err := config.LoadGollamaConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
			os.Exit(1)
		}
		if err := commands.ConfigCmd(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "version":
		fmt.Println("gollama v0.1.0")
	default:
		if err := commands.Chat(args); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}
}
