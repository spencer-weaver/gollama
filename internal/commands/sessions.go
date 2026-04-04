package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func Sessions() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home dir: %w", err)
	}

	sessionsDir := filepath.Join(home, ".praxis", "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("no sessions found")
			return nil
		}
		return fmt.Errorf("reading sessions dir: %w", err)
	}

	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			name := strings.TrimSuffix(e.Name(), ".json")
			info, _ := e.Info()
			fmt.Printf("%-20s %s\n", name, info.ModTime().Format("2006-01-02 15:04"))
		}
	}
	return nil
}
