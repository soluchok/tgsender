package messages

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/soluchok/tgsender/pkg/accounts"
	"github.com/soluchok/tgsender/pkg/auth"
)

// Handler provides HTTP handlers for message operations
type Handler struct {
	sender       *Sender
	jobManager   *JobManager
	accountStore *accounts.Store
	auth         *auth.Handler
}

// NewHandler creates a new messages handler
func NewHandler(sender *Sender, jobStore *JobStore, accountStore *accounts.Store, authHandler *auth.Handler) *Handler {
	return &Handler{
		sender:       sender,
		jobManager:   NewJobManager(jobStore, sender),
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
		DelayMinMS int      `json:"delay_min_ms"` // Min delay between messages in milliseconds
		DelayMaxMS int      `json:"delay_max_ms"` // Max delay between messages in milliseconds
		AIPrompt   string   `json:"ai_prompt"`    // AI prompt for message rewriting
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

	// Cap delays at 60 seconds and ensure valid range
	if req.DelayMinMS < 0 {
		req.DelayMinMS = 0
	}
	if req.DelayMaxMS < 0 {
		req.DelayMaxMS = 0
	}
	if req.DelayMinMS > 60000 {
		req.DelayMinMS = 60000
	}
	if req.DelayMaxMS > 60000 {
		req.DelayMaxMS = 60000
	}
	if req.DelayMinMS > req.DelayMaxMS {
		req.DelayMinMS = req.DelayMaxMS
	}

	// Get session path
	sessionPath := ".data/account_" + account.SessionToken + ".json"
	if account.SessionToken == "" {
		writeJSONError(w, "Account session not found - please re-authenticate", http.StatusBadRequest)
		return
	}

	// Get OpenAI token if AI prompt is provided
	var openAIToken string
	if req.AIPrompt != "" {
		openAIToken = account.OpenAIToken
		if openAIToken == "" {
			writeJSONError(w, "OpenAI token not configured for this account", http.StatusBadRequest)
			return
		}
	}

	// Start async send job
	job, err := h.jobManager.StartSend(accountID, sessionPath, req.Message, req.ContactIDs, req.DelayMinMS, req.DelayMaxMS, req.AIPrompt, openAIToken)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to start send job: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{
		"id":         job.ID,
		"account_id": job.AccountID,
		"status":     job.Status,
		"total":      job.Total,
		"sent":       job.Sent,
		"failed":     job.Failed,
	}, http.StatusOK)
}

// HandleSendStatus handles GET /api/accounts/{id}/send/status
func (h *Handler) HandleSendStatus(w http.ResponseWriter, r *http.Request) {
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

	// Get job ID from query param
	jobID := r.URL.Query().Get("job_id")
	if jobID == "" {
		writeJSONError(w, "job_id is required", http.StatusBadRequest)
		return
	}

	job, found := h.jobManager.GetJob(jobID)
	if !found {
		writeJSONError(w, "Job not found", http.StatusNotFound)
		return
	}

	// Verify job belongs to this account
	if job.AccountID != accountID {
		writeJSONError(w, "Job not found", http.StatusNotFound)
		return
	}

	writeJSON(w, job, http.StatusOK)
}

// HandleRetryFailed handles POST /api/accounts/{id}/send/retry
func (h *Handler) HandleRetryFailed(w http.ResponseWriter, r *http.Request) {
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
		JobID string `json:"job_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.JobID == "" {
		writeJSONError(w, "job_id is required", http.StatusBadRequest)
		return
	}

	// Verify original job belongs to this account
	oldJob, found := h.jobManager.GetJob(req.JobID)
	if !found {
		writeJSONError(w, "Job not found", http.StatusNotFound)
		return
	}

	if oldJob.AccountID != accountID {
		writeJSONError(w, "Job not found", http.StatusNotFound)
		return
	}

	if oldJob.Failed == 0 {
		writeJSONError(w, "No failed messages to retry", http.StatusBadRequest)
		return
	}

	// Start retry job
	job, err := h.jobManager.RetryFailed(req.JobID)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to start retry: %v", err), http.StatusInternalServerError)
		return
	}

	if job == nil {
		writeJSONError(w, "No failed messages to retry", http.StatusBadRequest)
		return
	}

	writeJSON(w, map[string]interface{}{
		"id":         job.ID,
		"account_id": job.AccountID,
		"status":     job.Status,
		"total":      job.Total,
		"sent":       job.Sent,
		"failed":     job.Failed,
	}, http.StatusOK)
}

// HandleSendHistory handles GET /api/accounts/{id}/send/history
func (h *Handler) HandleSendHistory(w http.ResponseWriter, r *http.Request) {
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

	// Get all jobs for this account
	jobs := h.jobManager.GetJobsByAccount(accountID)

	writeJSON(w, map[string]interface{}{
		"jobs": jobs,
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
