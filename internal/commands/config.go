package commands

import (
	"encoding/json"
	"os"
)

func ConfigCmd(cfg any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(cfg)
}
