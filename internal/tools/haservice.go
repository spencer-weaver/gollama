package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// HAServiceTool calls the Home Assistant REST API to trigger a service action.
type HAServiceTool struct {
	haHost string
	token  string
	client *http.Client
}

// NewHAServiceTool returns an HAServiceTool configured for the given HA host and token.
func NewHAServiceTool(haHost, token string) *HAServiceTool {
	return &HAServiceTool{
		haHost: haHost,
		token:  token,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (t *HAServiceTool) Name() string { return "call_ha_service" }
func (t *HAServiceTool) Description() string {
	return `Call a Home Assistant service to control a device. Args: {"domain": "light", "service": "turn_on", "entity_id": "light.living_room", "service_data": {}}`
}

func (t *HAServiceTool) Execute(args map[string]any) (string, error) {
	domain, _ := args["domain"].(string)
	service, _ := args["service"].(string)
	if domain == "" || service == "" {
		return "", fmt.Errorf("domain and service are required")
	}

	// Build the service_data payload, merging entity_id in if provided.
	payload := map[string]any{}
	if sd, ok := args["service_data"].(map[string]any); ok {
		for k, v := range sd {
			payload[k] = v
		}
	}
	if entityID, ok := args["entity_id"].(string); ok && entityID != "" {
		payload["entity_id"] = entityID
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("call_ha_service: marshal payload: %w", err)
	}

	endpoint := fmt.Sprintf("%s/api/services/%s/%s", t.haHost, domain, service)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewBuffer(body))
	if err != nil {
		return "", fmt.Errorf("call_ha_service: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("call_ha_service: HA unreachable (%s): %w", t.haHost, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusMultiStatus {
		return "OK", nil
	}
	return "", fmt.Errorf("call_ha_service: HA returned %d", resp.StatusCode)
}
