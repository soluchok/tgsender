package contacts

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/soluchok/tgsender/pkg/accounts"
	"github.com/soluchok/tgsender/pkg/auth"
)

// Handler provides HTTP handlers for contacts management
type Handler struct {
	store        *Store
	checker      *Checker
	accountStore *accounts.Store
	auth         *auth.Handler
}

// NewHandler creates a new contacts handler
func NewHandler(store *Store, checker *Checker, accountStore *accounts.Store, authHandler *auth.Handler) *Handler {
	return &Handler{
		store:        store,
		checker:      checker,
		accountStore: accountStore,
		auth:         authHandler,
	}
}

// HandleCheckNumbers handles POST /api/accounts/{id}/check-numbers
func (h *Handler) HandleCheckNumbers(w http.ResponseWriter, r *http.Request) {
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
		Phones []string `json:"phones"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.Phones) == 0 {
		writeJSONError(w, "No phone numbers provided", http.StatusBadRequest)
		return
	}

	// Deduplicate and clean phone numbers
	phoneSet := make(map[string]bool)
	var phones []string
	for _, phone := range req.Phones {
		if phone != "" && !phoneSet[phone] {
			phoneSet[phone] = true
			phones = append(phones, phone)
		}
	}

	// Get session path for this account
	sessionPath := fmt.Sprintf(".session/account_%s.json", account.SessionToken)
	if account.SessionToken == "" {
		// Fallback for accounts created before session token tracking
		sessionPath = fmt.Sprintf(".session/account_%s.json", accountID)
	}

	// Check numbers
	result, err := h.checker.CheckNumbers(r.Context(), accountID, sessionPath, phones)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{
		"valid":       result.Valid,
		"invalid":     result.Invalid,
		"retry":       result.Retry,
		"errors":      result.Errors,
		"total":       len(phones),
		"valid_count": len(result.Valid),
	}, http.StatusOK)
}

// HandleListContacts handles GET /api/accounts/{id}/contacts
func (h *Handler) HandleListContacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
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

	// Get contacts
	validOnly := r.URL.Query().Get("valid") == "true"
	var contacts []*Contact
	if validOnly {
		contacts = h.store.GetValidByAccount(accountID)
	} else {
		contacts = h.store.GetByAccount(accountID)
	}

	if contacts == nil {
		contacts = []*Contact{}
	}

	writeJSON(w, map[string]interface{}{
		"contacts": contacts,
		"count":    len(contacts),
	}, http.StatusOK)
}

// HandleDeleteContact handles DELETE /api/contacts/{id}
func (h *Handler) HandleDeleteContact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ownerID, ok := h.getOwnerID(r)
	if !ok {
		writeJSONError(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	// Get contact ID from path
	contactID := r.PathValue("id")
	if contactID == "" {
		writeJSONError(w, "Contact ID required", http.StatusBadRequest)
		return
	}

	// Get contact and verify ownership through account
	contact, ok := h.store.Get(contactID)
	if !ok {
		writeJSONError(w, "Contact not found", http.StatusNotFound)
		return
	}

	// Verify the account belongs to this owner
	account, ok := h.accountStore.Get(contact.AccountID)
	if !ok || account.OwnerID != ownerID {
		writeJSONError(w, "Unauthorized", http.StatusForbidden)
		return
	}

	if err := h.store.Delete(contactID); err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{"message": "Contact deleted"}, http.StatusOK)
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
