package contacts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"github.com/soluchok/tgsender/pkg/accounts"
	"github.com/soluchok/tgsender/pkg/auth"
)

// Handler provides HTTP handlers for contacts management
type Handler struct {
	store        *Store
	checker      *Checker
	accountStore *accounts.Store
	auth         *auth.Handler
	jobManager   *JobManager
}

// NewHandler creates a new contacts handler
func NewHandler(store *Store, checker *Checker, accountStore *accounts.Store, authHandler *auth.Handler, jobManager *JobManager) *Handler {
	return &Handler{
		store:        store,
		checker:      checker,
		accountStore: accountStore,
		auth:         authHandler,
		jobManager:   jobManager,
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
		Phones    []string `json:"phones"`
		Usernames []string `json:"usernames"`
		Inputs    []string `json:"inputs"` // Mixed inputs (auto-detect phone vs username)
		Labels    []string `json:"labels"` // Custom labels to apply to contacts
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Process mixed inputs - auto-detect phones vs usernames
	for _, input := range req.Inputs {
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		if strings.HasPrefix(input, "@") || (!strings.HasPrefix(input, "+") && !isNumeric(input)) {
			req.Usernames = append(req.Usernames, input)
		} else {
			req.Phones = append(req.Phones, input)
		}
	}

	if len(req.Phones) == 0 && len(req.Usernames) == 0 {
		writeJSONError(w, "No phone numbers or usernames provided", http.StatusBadRequest)
		return
	}

	// Deduplicate and clean inputs
	phoneSet := make(map[string]bool)
	var phones []string
	for _, phone := range req.Phones {
		phone = strings.TrimSpace(phone)
		if phone != "" && !phoneSet[phone] {
			phoneSet[phone] = true
			phones = append(phones, phone)
		}
	}

	usernameSet := make(map[string]bool)
	var usernames []string
	for _, username := range req.Usernames {
		username = strings.TrimSpace(username)
		username = strings.TrimPrefix(username, "@")
		if username != "" && !usernameSet[strings.ToLower(username)] {
			usernameSet[strings.ToLower(username)] = true
			usernames = append(usernames, username)
		}
	}

	// Get session path for this account
	sessionPath := fmt.Sprintf(".data/account_%s.json", account.SessionToken)
	if account.SessionToken == "" {
		// Fallback for accounts created before session token tracking
		sessionPath = fmt.Sprintf(".data/account_%s.json", accountID)
	}

	// Check contacts
	input := &CheckInput{
		Phones:    phones,
		Usernames: usernames,
		Labels:    req.Labels,
	}
	result, err := h.checker.CheckContacts(r.Context(), accountID, sessionPath, input)
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

	collator := collate.New(language.English)
	slices.SortFunc(contacts, func(a, b *Contact) int {
		nameA := strings.TrimSpace(a.FirstName + " " + a.LastName)
		nameB := strings.TrimSpace(b.FirstName + " " + b.LastName)
		return collator.CompareString(nameA, nameB)
	})

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

// HandleUpdateContact handles PUT /api/contacts/{id}
func (h *Handler) HandleUpdateContact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
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

	// Parse request body
	var req struct {
		FirstName string   `json:"first_name"`
		LastName  string   `json:"last_name"`
		Labels    []string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Update contact
	if err := h.store.Update(contactID, req.FirstName, req.LastName, req.Labels); err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return updated contact
	updatedContact, _ := h.store.Get(contactID)
	writeJSON(w, updatedContact, http.StatusOK)
}

// HandleImportFromChats handles POST /api/accounts/{id}/import-chats
func (h *Handler) HandleImportFromChats(w http.ResponseWriter, r *http.Request) {
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

	// Get session path for this account
	sessionPath := fmt.Sprintf(".data/account_%s.json", account.SessionToken)
	if account.SessionToken == "" {
		sessionPath = fmt.Sprintf(".data/account_%s.json", accountID)
	}

	// Start async import job
	job, isNew := h.jobManager.StartImport(accountID, sessionPath)

	writeJSON(w, map[string]interface{}{
		"id":         job.ID,
		"account_id": job.AccountID,
		"status":     job.Status,
		"progress":   job.Progress,
		"imported":   job.Imported,
		"skipped":    job.Skipped,
		"is_new":     isNew,
	}, http.StatusOK)
}

// HandleImportContacts handles POST /api/accounts/{id}/import-contacts
func (h *Handler) HandleImportContacts(w http.ResponseWriter, r *http.Request) {
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

	// Get session path for this account
	sessionPath := fmt.Sprintf(".data/account_%s.json", account.SessionToken)
	if account.SessionToken == "" {
		sessionPath = fmt.Sprintf(".data/account_%s.json", accountID)
	}

	// Start async import job
	job, isNew := h.jobManager.StartImportContacts(accountID, sessionPath)

	writeJSON(w, map[string]interface{}{
		"id":          job.ID,
		"account_id":  job.AccountID,
		"import_type": job.ImportType,
		"status":      job.Status,
		"progress":    job.Progress,
		"imported":    job.Imported,
		"skipped":     job.Skipped,
		"is_new":      isNew,
	}, http.StatusOK)
}

// HandleImportFromChatsStatus handles GET /api/accounts/{id}/import-chats/status
// If job_id is provided, returns that specific job's status
// If job_id is not provided, returns the active job for the account (if any)
func (h *Handler) HandleImportFromChatsStatus(w http.ResponseWriter, r *http.Request) {
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

	// Get job ID from query param (optional)
	jobID := r.URL.Query().Get("job_id")

	var job *ImportJob
	var found bool

	if jobID != "" {
		// Get specific job by ID
		job, found = h.jobManager.GetJob(jobID)
		if !found {
			writeJSONError(w, "Job not found", http.StatusNotFound)
			return
		}
		// Verify job belongs to this account
		if job.AccountID != accountID {
			writeJSONError(w, "Job not found", http.StatusNotFound)
			return
		}
	} else {
		// Get active job for account
		job, found = h.jobManager.GetJobByAccount(accountID)
		if !found {
			// No active job - return empty response
			writeJSON(w, map[string]interface{}{
				"active": false,
			}, http.StatusOK)
			return
		}
	}

	writeJSON(w, map[string]interface{}{
		"active":   true,
		"id":       job.ID,
		"status":   job.Status,
		"progress": job.Progress,
		"imported": job.Imported,
		"skipped":  job.Skipped,
		"error":    job.Error,
	}, http.StatusOK)
}

// HandleExportContacts handles POST /api/contacts/export
func (h *Handler) HandleExportContacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ownerID, ok := h.getOwnerID(r)
	if !ok {
		writeJSONError(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	// Parse request body
	var req struct {
		AccountIDs []string `json:"account_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.AccountIDs) == 0 {
		writeJSONError(w, "At least one account ID is required", http.StatusBadRequest)
		return
	}

	// Verify all accounts exist and belong to this owner
	for _, accountID := range req.AccountIDs {
		account, ok := h.accountStore.Get(accountID)
		if !ok {
			writeJSONError(w, fmt.Sprintf("Account not found: %s", accountID), http.StatusNotFound)
			return
		}
		if account.OwnerID != ownerID {
			writeJSONError(w, "Unauthorized", http.StatusForbidden)
			return
		}
	}

	// Collect contacts from all selected accounts
	var allContacts []*Contact
	for _, accountID := range req.AccountIDs {
		contacts := h.store.GetByAccount(accountID)
		allContacts = append(allContacts, contacts...)
	}

	// Sort contacts by name
	collator := collate.New(language.English)
	slices.SortFunc(allContacts, func(a, b *Contact) int {
		nameA := strings.TrimSpace(a.FirstName + " " + a.LastName)
		nameB := strings.TrimSpace(b.FirstName + " " + b.LastName)
		return collator.CompareString(nameA, nameB)
	})

	// Set headers for file download
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=contacts.json")

	// Write JSON
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.Encode(allContacts)
}

// HandleImportFromFile handles POST /api/accounts/{id}/import-file
func (h *Handler) HandleImportFromFile(w http.ResponseWriter, r *http.Request) {
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

	// Parse request body - array of contacts to import
	var req struct {
		Contacts []FileImportContact `json:"contacts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.Contacts) == 0 {
		writeJSONError(w, "No contacts provided", http.StatusBadRequest)
		return
	}

	// Get session path for this account
	sessionPath := fmt.Sprintf(".data/account_%s.json", account.SessionToken)
	if account.SessionToken == "" {
		sessionPath = fmt.Sprintf(".data/account_%s.json", accountID)
	}

	// Import contacts
	result, err := h.checker.ImportFromFile(r.Context(), accountID, sessionPath, req.Contacts)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{
		"imported": result.Imported,
		"skipped":  result.Skipped,
		"failed":   result.Failed,
		"errors":   result.Errors,
	}, http.StatusOK)
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

// isNumeric checks if a string contains only digits (for phone number detection)
func isNumeric(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return len(s) > 0
}
