package handler

import (
	"encoding/json"
	"log"
	"net/http"
)

// fwdRequest is the body ConversationForwarder sends.
type fwdRequest struct {
	Text           string `json:"text"`
	ConversationID string `json:"conversation_id"`
	DeviceID       string `json:"device_id"`
	Language       string `json:"language"`
	AgentID        string `json:"agent_id"`
}

// fwdResponse is the envelope ConversationForwarder expects back.
// HTTP status must always be 200; errors are signalled via finish_reason.
type fwdResponse struct {
	Message              string `json:"message"`
	ContinueConversation bool   `json:"continue_conversation"`
	FinishReason         string `json:"finish_reason"`
}

// NewForwardHandler returns an http.HandlerFunc for POST /forward.
// It reuses the Processor interface defined in process.go.
func NewForwardHandler(p Processor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		encode := func(resp fwdResponse) {
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				log.Printf("forward: encode response: %v", err)
			}
		}

		var req fwdRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			encode(fwdResponse{
				Message:              "Sorry, something went wrong.",
				ContinueConversation: false,
				FinishReason:         "error",
			})
			return
		}

		speech, err := p.ProcessTurn(req.ConversationID, req.Text)
		if err != nil {
			log.Printf("forward: process turn [%s]: %v", req.ConversationID, err)
			encode(fwdResponse{
				Message:              "Sorry, something went wrong.",
				ContinueConversation: false,
				FinishReason:         "error",
			})
			return
		}

		encode(fwdResponse{
			Message:              speech,
			ContinueConversation: false,
			FinishReason:         "done",
		})
	}
}
