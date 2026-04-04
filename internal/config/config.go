package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const DefaultRegistryDir = "~/.praxis/bin"

func ExpandHome(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, path[1:]), nil
}

var GlobalConfigPath string

func SetGlobalPath(path string) {
	GlobalConfigPath = path
}

func GetGlobalPath() string {
	return GlobalConfigPath
}

func LoadConfigData(data any, cfg any) error {
	switch v := data.(type) {
	case []byte:
		return json.Unmarshal(v, cfg)
	case string:
		f, err := os.Open(v)
		if err != nil {
			return fmt.Errorf("open config %s: %w", v, err)
		}
		defer f.Close()
		return json.NewDecoder(f).Decode(cfg)
	default:
		return fmt.Errorf("unsupported config source type %T", v)
	}
}

// LoadGollamaConfig loads gollama.json, trying the exe-relative path first,
// falling back to ./config/gollama.json.
func LoadGollamaConfig() (*GollamaConfig, error) {
	path := filepath.Join("config", "gollama.json")
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "..", "..", "config", "gollama.json")
		if _, err := os.Stat(candidate); err == nil {
			path = candidate
		}
	}
	var cfg GollamaConfig
	if err := LoadConfigData(path, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func SaveConfigFile[T any](path string, cfg *T) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
