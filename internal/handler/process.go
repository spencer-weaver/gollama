// Package handler provides the HTTP handler for the Home Assistant
// conversation agent API endpoint.
package handler

import (
	"encoding/json"
	"log"
	"net/http"
)

// haRequest is the body HA sends to POST /api/conversation/process.
type haRequest struct {
	Text           string `json:"text"`
	ConversationID string `json:"conversation_id"`
	Language       string `json:"language"`
	AgentID        string `json:"agent_id"`
}

// haResponse is the envelope HA expects back.
type haResponse struct {
	Response       responseBody `json:"response"`
	ConversationID string       `json:"conversation_id"`
}

type responseBody struct {
	ResponseType string     `json:"response_type"`
	Speech       speechBody `json:"speech"`
}

type speechBody struct {
	Plain plainSpeech `json:"plain"`
}

type plainSpeech struct {
	Speech string `json:"speech"`
}

// Processor can handle one conversation turn.
type Processor interface {
	ProcessTurn(convID, text string) (string, error)
}

// NewProcessHandler returns an http.HandlerFunc for POST /api/conversation/process.
func NewProcessHandler(p Processor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req haRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Text == "" {
			http.Error(w, "text is required", http.StatusBadRequest)
			return
		}

		speech, err := p.ProcessTurn(req.ConversationID, req.Text)
		if err != nil {
			log.Printf("process turn [%s]: %v", req.ConversationID, err)
			speech = "Sorry, something went wrong. Please try again."
		}

		resp := haResponse{
			ConversationID: req.ConversationID,
			Response: responseBody{
				ResponseType: "action_done",
				Speech: speechBody{
					Plain: plainSpeech{Speech: speech},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("encode response: %v", err)
		}
	}
}
