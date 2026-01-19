package messages

import (
	"encoding/json"
	"net/http"

	"github.com/soluchok/tgsender/pkg/accounts"
	"github.com/soluchok/tgsender/pkg/auth"
)

// Handler provides HTTP handlers for message operations
type Handler struct {
	sender       *Sender
	accountStore *accounts.Store
	auth         *auth.Handler
}

// NewHandler creates a new messages handler
func NewHandler(sender *Sender, accountStore *accounts.Store, authHandler *auth.Handler) *Handler {
	return &Handler{
		sender:       sender,
		accountStore: accountStore,
		auth:         authHandler,
	}
}

// HandleSendMessages handles POST /api/accounts/{id}/send
func (h *Handler) HandleSendMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ownerID, ok := h.getOwnerID(r)
	if !ok {
		writeJSONError(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	// Get account ID from path
	accountID := r.PathValue("id")
	if accountID == "" {
		writeJSONError(w, "Account ID required", http.StatusBadRequest)
		return
	}

	// Verify account exists and belongs to this owner
	account, ok := h.accountStore.Get(accountID)
	if !ok {
		writeJSONError(w, "Account not found", http.StatusNotFound)
		return
	}

	if account.OwnerID != ownerID {
		writeJSONError(w, "Unauthorized", http.StatusForbidden)
		return
	}

	// Parse request body
	var req struct {
		ContactIDs []string `json:"contact_ids"`
		Message    string   `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.ContactIDs) == 0 {
		writeJSONError(w, "No contacts specified", http.StatusBadRequest)
		return
	}

	if req.Message == "" {
		writeJSONError(w, "Message is required", http.StatusBadRequest)
		return
	}

	// Get session path
	sessionPath := ".session/account_" + account.SessionToken + ".json"
	if account.SessionToken == "" {
		writeJSONError(w, "Account session not found - please re-authenticate", http.StatusBadRequest)
		return
	}

	// Send messages
	result, err := h.sender.SendToContacts(r.Context(), sessionPath, req.ContactIDs, req.Message)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, result, http.StatusOK)
}

func (h *Handler) getOwnerID(r *http.Request) (int64, bool) {
	cookie, err := r.Cookie("session_token")
	if err != nil {
		return 0, false
	}

	session, ok := h.auth.GetSession(cookie.Value)
	if !ok || session.User == nil {
		return 0, false
	}

	return session.User.ID, true
}

// Helper functions for JSON responses
func writeJSON(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeJSONError(w http.ResponseWriter, message string, status int) {
	writeJSON(w, map[string]string{"error": message}, status)
}
