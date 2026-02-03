package contacts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// JobStatus represents the status of an import job
type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
)

// ImportType represents the type of import
type ImportType string

const (
	ImportTypeChats    ImportType = "chats"
	ImportTypeContacts ImportType = "contacts"
)

// ImportJob represents an async import job
type ImportJob struct {
	ID         string     `json:"id"`
	AccountID  string     `json:"account_id"`
	ImportType ImportType `json:"import_type"`
	Status     JobStatus  `json:"status"`
	Progress   int        `json:"progress"` // Number of dialogs processed
	Imported   int        `json:"imported"` // Number of contacts imported
	Skipped    int        `json:"skipped"`  // Number of contacts skipped
	Error      string     `json:"error,omitempty"`
	ProxyURL   string     `json:"-"` // Proxy URL for Telegram connection (not exposed in JSON)
	StartedAt  time.Time  `json:"started_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// JobManager manages async import jobs
type JobManager struct {
	mu      sync.RWMutex
	jobs    map[string]*ImportJob // job ID -> job
	byAcct  map[string]string     // account ID -> job ID (for active jobs only)
	checker *Checker
}

// NewJobManager creates a new job manager
func NewJobManager(checker *Checker) *JobManager {
	return &JobManager{
		jobs:    make(map[string]*ImportJob),
		byAcct:  make(map[string]string),
		checker: checker,
	}
}

// StartImport starts an import job for an account, or returns existing running job
func (m *JobManager) StartImport(accountID, sessionPath, proxyURL string) (*ImportJob, bool) {
	return m.startImportWithType(accountID, sessionPath, proxyURL, ImportTypeChats)
}

// StartImportContacts starts an import contacts job for an account
func (m *JobManager) StartImportContacts(accountID, sessionPath, proxyURL string) (*ImportJob, bool) {
	return m.startImportWithType(accountID, sessionPath, proxyURL, ImportTypeContacts)
}

func (m *JobManager) startImportWithType(accountID, sessionPath, proxyURL string, importType ImportType) (*ImportJob, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if there's already a running job for this account
	if jobID, exists := m.byAcct[accountID]; exists {
		if job, ok := m.jobs[jobID]; ok {
			if job.Status == JobStatusPending || job.Status == JobStatusRunning {
				return job, false // Return existing job, not newly created
			}
		}
	}

	// Create new job
	jobID := generateJobID()
	job := &ImportJob{
		ID:         jobID,
		AccountID:  accountID,
		ImportType: importType,
		Status:     JobStatusPending,
		ProxyURL:   proxyURL,
		StartedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	m.jobs[jobID] = job
	m.byAcct[accountID] = jobID

	// Start the job in background
	go m.runImport(job, sessionPath)

	return job, true // Return new job
}

// GetJob returns a job by ID
func (m *JobManager) GetJob(jobID string) (*ImportJob, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	job, ok := m.jobs[jobID]
	if !ok {
		return nil, false
	}

	// Return a copy to avoid race conditions
	jobCopy := *job
	return &jobCopy, true
}

// GetJobByAccount returns the active job for an account
func (m *JobManager) GetJobByAccount(accountID string) (*ImportJob, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	jobID, exists := m.byAcct[accountID]
	if !exists {
		return nil, false
	}

	job, ok := m.jobs[jobID]
	if !ok {
		return nil, false
	}

	// Return a copy
	jobCopy := *job
	return &jobCopy, true
}

func (m *JobManager) runImport(job *ImportJob, sessionPath string) {
	// Update status to running
	m.mu.Lock()
	job.Status = JobStatusRunning
	job.UpdatedAt = time.Now()
	m.mu.Unlock()

	// Create a context with timeout (10 minutes max)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()

	var result *ChatContactsResult
	var err error

	if job.ImportType == ImportTypeContacts {
		// Import from Telegram contacts
		result, err = m.checker.ImportFromContacts(ctx, job.AccountID, sessionPath, job.ProxyURL, func(imported, skipped int) {
			m.mu.Lock()
			job.Imported = imported
			job.Skipped = skipped
			job.UpdatedAt = time.Now()
			m.mu.Unlock()
		})
	} else {
		// Import from chats (default)
		result, err = m.checker.ImportFromChatsWithProgress(ctx, job.AccountID, sessionPath, job.ProxyURL, func(progress, imported, skipped int) {
			m.mu.Lock()
			job.Progress = progress
			job.Imported = imported
			job.Skipped = skipped
			job.UpdatedAt = time.Now()
			m.mu.Unlock()
		})
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if err != nil {
		job.Status = JobStatusFailed
		job.Error = err.Error()
	} else {
		job.Status = JobStatusCompleted
		job.Imported = result.Imported
		job.Skipped = result.Skipped
		if len(result.Errors) > 0 {
			job.Error = result.Errors[0]
		}
	}
	job.UpdatedAt = time.Now()

	// Clean up account mapping after completion (allow new jobs)
	// Keep the job in jobs map for status queries, but remove from byAcct
	// so a new job can be started
	delete(m.byAcct, job.AccountID)

	// Schedule cleanup of old job after 5 minutes
	go func() {
		time.Sleep(5 * time.Minute)
		m.mu.Lock()
		delete(m.jobs, job.ID)
		m.mu.Unlock()
	}()
}

func generateJobID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
