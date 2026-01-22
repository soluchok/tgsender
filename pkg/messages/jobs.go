package messages

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// JobStatus represents the status of a send job
type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
)

// SendJob represents an async message sending job
type SendJob struct {
	ID          string            `json:"id"`
	AccountID   string            `json:"account_id"`
	Status      JobStatus         `json:"status"`
	Message     string            `json:"message"`
	DelayMinMS  int               `json:"delay_min_ms"`
	DelayMaxMS  int               `json:"delay_max_ms"`
	Total       int               `json:"total"`
	Sent        int               `json:"sent"`
	Failed      int               `json:"failed"`
	Results     []RecipientResult `json:"results"`
	Error       string            `json:"error,omitempty"`
	StartedAt   time.Time         `json:"started_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	ContactIDs  []string          `json:"contact_ids"`  // Original contact IDs
	SessionPath string            `json:"session_path"` // Session path for retries
}

// GetFailedContactIDs returns the contact IDs that failed to receive the message
func (j *SendJob) GetFailedContactIDs() []string {
	var failed []string
	for _, r := range j.Results {
		if !r.Success {
			failed = append(failed, r.ContactID)
		}
	}
	return failed
}

// JobStore manages persistent storage of send jobs
type JobStore struct {
	mu      sync.RWMutex
	dataDir string
	jobs    map[string]*SendJob // job ID -> job
}

// NewJobStore creates a new job store
func NewJobStore(dataDir string) (*JobStore, error) {
	store := &JobStore{
		dataDir: dataDir,
		jobs:    make(map[string]*SendJob),
	}

	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	if err := store.load(); err != nil {
		return nil, fmt.Errorf("failed to load jobs: %w", err)
	}

	return store, nil
}

// Get returns a job by ID
func (s *JobStore) Get(id string) (*SendJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	job, ok := s.jobs[id]
	if !ok {
		return nil, false
	}

	// Return a copy
	jobCopy := *job
	jobCopy.Results = make([]RecipientResult, len(job.Results))
	copy(jobCopy.Results, job.Results)
	jobCopy.ContactIDs = make([]string, len(job.ContactIDs))
	copy(jobCopy.ContactIDs, job.ContactIDs)
	return &jobCopy, true
}

// GetByAccount returns all jobs for an account
func (s *JobStore) GetByAccount(accountID string) []*SendJob {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var jobs []*SendJob
	for _, job := range s.jobs {
		if job.AccountID == accountID {
			jobCopy := *job
			jobCopy.Results = make([]RecipientResult, len(job.Results))
			copy(jobCopy.Results, job.Results)
			jobCopy.ContactIDs = make([]string, len(job.ContactIDs))
			copy(jobCopy.ContactIDs, job.ContactIDs)
			jobs = append(jobs, &jobCopy)
		}
	}
	return jobs
}

// Create adds a new job
func (s *JobStore) Create(job *SendJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if job.ID == "" {
		job.ID = generateJobID()
	}

	s.jobs[job.ID] = job
	return s.save()
}

// Update updates an existing job
func (s *JobStore) Update(job *SendJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.jobs[job.ID]; !ok {
		return fmt.Errorf("job not found: %s", job.ID)
	}

	s.jobs[job.ID] = job
	return s.save()
}

// UpdateProgress updates job progress without full save (in-memory only during sending)
func (s *JobStore) UpdateProgress(jobID string, sent, failed int, results []RecipientResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if job, ok := s.jobs[jobID]; ok {
		job.Sent = sent
		job.Failed = failed
		job.Results = results
		job.UpdatedAt = time.Now()
	}
}

// SetStatus updates job status and saves
func (s *JobStore) SetStatus(jobID string, status JobStatus, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return fmt.Errorf("job not found: %s", jobID)
	}

	job.Status = status
	job.Error = errMsg
	job.UpdatedAt = time.Now()
	return s.save()
}

// FinalizeJob saves the final job state
func (s *JobStore) FinalizeJob(jobID string, status JobStatus, sent, failed int, results []RecipientResult, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return fmt.Errorf("job not found: %s", jobID)
	}

	job.Status = status
	job.Sent = sent
	job.Failed = failed
	job.Results = results
	job.Error = errMsg
	job.UpdatedAt = time.Now()
	return s.save()
}

// Delete removes a job
func (s *JobStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.jobs, id)
	return s.save()
}

// Cleanup removes old completed/failed jobs (keep last N per account)
func (s *JobStore) Cleanup(maxPerAccount int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Group jobs by account
	byAccount := make(map[string][]*SendJob)
	for _, job := range s.jobs {
		byAccount[job.AccountID] = append(byAccount[job.AccountID], job)
	}

	// For each account, keep only the most recent jobs
	for _, jobs := range byAccount {
		if len(jobs) <= maxPerAccount {
			continue
		}

		// Sort by started_at descending
		for i := 0; i < len(jobs)-1; i++ {
			for j := i + 1; j < len(jobs); j++ {
				if jobs[j].StartedAt.After(jobs[i].StartedAt) {
					jobs[i], jobs[j] = jobs[j], jobs[i]
				}
			}
		}

		// Delete old jobs
		for i := maxPerAccount; i < len(jobs); i++ {
			delete(s.jobs, jobs[i].ID)
		}
	}

	return s.save()
}

func (s *JobStore) load() error {
	filePath := filepath.Join(s.dataDir, "jobs.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var jobs []*SendJob
	if err := json.Unmarshal(data, &jobs); err != nil {
		return err
	}

	for _, job := range jobs {
		// Reset any running jobs to failed (server restart)
		if job.Status == JobStatusRunning || job.Status == JobStatusPending {
			job.Status = JobStatusFailed
			job.Error = "interrupted by server restart"
		}
		s.jobs[job.ID] = job
	}

	return nil
}

func (s *JobStore) save() error {
	jobs := make([]*SendJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}

	data, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return err
	}

	filePath := filepath.Join(s.dataDir, "jobs.json")
	return os.WriteFile(filePath, data, 0600)
}

// JobManager manages async send jobs
type JobManager struct {
	store  *JobStore
	sender *Sender
}

// NewJobManager creates a new job manager
func NewJobManager(store *JobStore, sender *Sender) *JobManager {
	return &JobManager{
		store:  store,
		sender: sender,
	}
}

// StartSend starts a send job for an account
func (m *JobManager) StartSend(accountID, sessionPath, message string, contactIDs []string, delayMinMS, delayMaxMS int) (*SendJob, error) {
	job := &SendJob{
		ID:          generateJobID(),
		AccountID:   accountID,
		Status:      JobStatusPending,
		Message:     message,
		DelayMinMS:  delayMinMS,
		DelayMaxMS:  delayMaxMS,
		Total:       len(contactIDs),
		Results:     make([]RecipientResult, 0),
		StartedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		ContactIDs:  contactIDs,
		SessionPath: sessionPath,
	}

	if err := m.store.Create(job); err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}

	// Cleanup old jobs (keep last 50 per account)
	go m.store.Cleanup(50)

	// Start the job in background
	go m.runSend(job.ID)

	return job, nil
}

// RetryFailed retries sending to failed contacts from a previous job
func (m *JobManager) RetryFailed(jobID string) (*SendJob, error) {
	oldJob, ok := m.store.Get(jobID)
	if !ok {
		return nil, fmt.Errorf("job not found")
	}

	// Get failed contact IDs
	failedIDs := oldJob.GetFailedContactIDs()
	if len(failedIDs) == 0 {
		return nil, fmt.Errorf("no failed contacts to retry")
	}

	// Create new job for retry
	job := &SendJob{
		ID:          generateJobID(),
		AccountID:   oldJob.AccountID,
		Status:      JobStatusPending,
		Message:     oldJob.Message,
		DelayMinMS:  oldJob.DelayMinMS,
		DelayMaxMS:  oldJob.DelayMaxMS,
		Total:       len(failedIDs),
		Results:     make([]RecipientResult, 0),
		StartedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		ContactIDs:  failedIDs,
		SessionPath: oldJob.SessionPath,
	}

	if err := m.store.Create(job); err != nil {
		return nil, fmt.Errorf("failed to create retry job: %w", err)
	}

	// Start the job in background
	go m.runSend(job.ID)

	return job, nil
}

// GetJob returns a job by ID
func (m *JobManager) GetJob(jobID string) (*SendJob, bool) {
	return m.store.Get(jobID)
}

// GetJobsByAccount returns all jobs for an account
func (m *JobManager) GetJobsByAccount(accountID string) []*SendJob {
	return m.store.GetByAccount(accountID)
}

func (m *JobManager) runSend(jobID string) {
	// Get the job
	job, ok := m.store.Get(jobID)
	if !ok {
		slog.Error("job not found for sending", "job_id", jobID)
		return
	}

	// Update status to running
	if err := m.store.SetStatus(jobID, JobStatusRunning, ""); err != nil {
		slog.Error("failed to update job status", "job_id", jobID, "error", err)
		return
	}

	// Create a context with timeout (1 hour max)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Hour)
	defer cancel()

	// Run the send with progress callback
	result, err := m.sender.SendToContactsWithProgress(ctx, job.SessionPath, job.ContactIDs, job.Message, job.DelayMinMS, job.DelayMaxMS, func(sent, failed int, results []RecipientResult) {
		m.store.UpdateProgress(jobID, sent, failed, results)
	})

	// Finalize the job
	var status JobStatus
	var errMsg string
	var sent, failed int
	var results []RecipientResult

	if err != nil {
		status = JobStatusFailed
		errMsg = err.Error()
		// Keep whatever progress we had
		if currentJob, ok := m.store.Get(jobID); ok {
			sent = currentJob.Sent
			failed = currentJob.Failed
			results = currentJob.Results
		}
	} else {
		status = JobStatusCompleted
		sent = result.Successful
		failed = result.Failed
		results = result.Results
	}

	if err := m.store.FinalizeJob(jobID, status, sent, failed, results, errMsg); err != nil {
		slog.Error("failed to finalize job", "job_id", jobID, "error", err)
	}
}

func generateJobID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
