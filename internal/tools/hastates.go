package tools

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// HAStatesTool fetches the current state of all Home Assistant entities so the
// model can discover entity IDs and check device states before acting.
type HAStatesTool struct {
	haHost string
	token  string
	client *http.Client
}

// NewHAStatesTool returns an HAStatesTool configured for the given HA host and token.
func NewHAStatesTool(haHost, token string) *HAStatesTool {
	return &HAStatesTool{
		haHost: haHost,
		token:  token,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (t *HAStatesTool) Name() string { return "get_ha_states" }
func (t *HAStatesTool) Description() string {
	return `Get the current state of all Home Assistant entities. Use this before calling call_ha_service if you need to find an entity ID or check the current state of a device.`
}

type haStateEntry struct {
	EntityID   string         `json:"entity_id"`
	State      string         `json:"state"`
	Attributes map[string]any `json:"attributes"`
}

func (t *HAStatesTool) Execute(args map[string]any) (string, error) {
	endpoint := t.haHost + "/api/states"
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("get_ha_states: build request: %w", err)
	}
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("get_ha_states: HA unreachable (%s): %w", t.haHost, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("get_ha_states: HA returned %d", resp.StatusCode)
	}

	var states []haStateEntry
	if err := json.NewDecoder(resp.Body).Decode(&states); err != nil {
		return "", fmt.Errorf("get_ha_states: decode response: %w", err)
	}

	var sb strings.Builder
	count := 0
	for _, s := range states {
		if count >= 100 {
			break
		}
		friendlyName, ok := s.Attributes["friendly_name"].(string)
		if !ok || friendlyName == "" {
			continue
		}
		fmt.Fprintf(&sb, "%s: %s (%s)\n", s.EntityID, s.State, friendlyName)
		count++
	}

	if count == 0 {
		return "No named entities found.", nil
	}
	return sb.String(), nil
}
