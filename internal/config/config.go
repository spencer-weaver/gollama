package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

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
