package accounts

import (
	"encoding/json"
	"net/http"

	"github.com/soluchok/tgsender/pkg/auth"
)

// Handler provides HTTP handlers for account management
type Handler struct {
	store     *Store
	qrManager *QRAuthManager
	auth      *auth.Handler
}

// NewHandler creates a new accounts handler
func NewHandler(store *Store, qrManager *QRAuthManager, authHandler *auth.Handler) *Handler {
	return &Handler{
		store:     store,
		qrManager: qrManager,
		auth:      authHandler,
	}
}

// HandleListAccounts handles GET /api/accounts
func (h *Handler) HandleListAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ownerID, ok := h.getOwnerID(r)
	if !ok {
		writeJSONError(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	accounts := h.store.GetByOwner(ownerID)
	if accounts == nil {
		accounts = []*Account{}
	}

	writeJSON(w, map[string]interface{}{
		"accounts": accounts,
	}, http.StatusOK)
}

// HandleDeleteAccount handles DELETE /api/accounts/{id}
func (h *Handler) HandleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ownerID, ok := h.getOwnerID(r)
	if !ok {
		writeJSONError(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	// Extract account ID from path
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, "Account ID required", http.StatusBadRequest)
		return
	}

	if err := h.store.Delete(id, ownerID); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, map[string]string{"message": "Account deleted"}, http.StatusOK)
}

// HandleStartQRAuth handles POST /api/accounts/qr/start
func (h *Handler) HandleStartQRAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ownerID, ok := h.getOwnerID(r)
	if !ok {
		writeJSONError(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	state, err := h.qrManager.StartAuth(ownerID)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, state, http.StatusOK)
}

// HandleQRAuthStatus handles GET /api/accounts/qr/status
func (h *Handler) HandleQRAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := r.URL.Query().Get("token")
	if token == "" {
		writeJSONError(w, "Token required", http.StatusBadRequest)
		return
	}

	state, ok := h.qrManager.GetStatus(token)
	if !ok {
		writeJSONError(w, "Session not found or expired", http.StatusNotFound)
		return
	}

	writeJSON(w, state, http.StatusOK)
}

// HandleCancelQRAuth handles POST /api/accounts/qr/cancel
func (h *Handler) HandleCancelQRAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	h.qrManager.CancelAuth(req.Token)
	writeJSON(w, map[string]string{"message": "Cancelled"}, http.StatusOK)
}

// HandleSubmitPassword handles POST /api/accounts/qr/password
func (h *Handler) HandleSubmitPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Token == "" || req.Password == "" {
		writeJSONError(w, "Token and password required", http.StatusBadRequest)
		return
	}

	if err := h.qrManager.SubmitPassword(req.Token, req.Password); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, map[string]string{"message": "Password submitted"}, http.StatusOK)
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
