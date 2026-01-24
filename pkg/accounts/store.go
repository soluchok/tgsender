package accounts

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Account represents a Telegram account linked to a user
type Account struct {
	ID           string    `json:"id"`
	OwnerID      int64     `json:"owner_id"`    // Telegram user ID of the owner (from OAuth)
	TelegramID   int64     `json:"telegram_id"` // Telegram user ID of this account
	Phone        string    `json:"phone"`
	FirstName    string    `json:"first_name"`
	LastName     string    `json:"last_name,omitempty"`
	Username     string    `json:"username,omitempty"`
	PhotoURL     string    `json:"photo_url,omitempty"`
	SessionToken string    `json:"session_token,omitempty"` // Token used for session file path
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	OpenAIToken  string    `json:"openai_token,omitempty"` // OpenAI API token for message rewriting
}

// Store manages account storage
type Store struct {
	mu       sync.RWMutex
	dataDir  string
	accounts map[string]*Account // keyed by account ID
}

// NewStore creates a new account store
func NewStore(dataDir string) (*Store, error) {
	store := &Store{
		dataDir:  dataDir,
		accounts: make(map[string]*Account),
	}

	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	if err := store.load(); err != nil {
		return nil, fmt.Errorf("failed to load accounts: %w", err)
	}

	return store, nil
}

// GetByOwner returns all accounts owned by a user, sorted by creation time
func (s *Store) GetByOwner(ownerID int64) []*Account {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var accounts []*Account
	for _, acc := range s.accounts {
		if acc.OwnerID == ownerID {
			accounts = append(accounts, acc)
		}
	}

	// Sort by creation time (oldest first)
	sort.Slice(accounts, func(i, j int) bool {
		return accounts[i].CreatedAt.Before(accounts[j].CreatedAt)
	})

	return accounts
}

// Get returns an account by ID
func (s *Store) Get(id string) (*Account, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	acc, ok := s.accounts[id]
	return acc, ok
}

// Create adds a new account
func (s *Store) Create(acc *Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for duplicate TelegramID for the same owner
	for _, existing := range s.accounts {
		if existing.OwnerID == acc.OwnerID && existing.TelegramID == acc.TelegramID {
			// Account already exists, update it instead of creating duplicate
			acc.ID = existing.ID
			acc.CreatedAt = existing.CreatedAt
			s.accounts[acc.ID] = acc
			return s.save()
		}
	}

	// Generate ID if not set
	if acc.ID == "" {
		id, err := generateID()
		if err != nil {
			return err
		}
		acc.ID = id
	}

	acc.CreatedAt = time.Now()
	s.accounts[acc.ID] = acc

	return s.save()
}

// Delete removes an account
func (s *Store) Delete(id string, ownerID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	acc, ok := s.accounts[id]
	if !ok {
		return fmt.Errorf("account not found")
	}

	// Verify ownership
	if acc.OwnerID != ownerID {
		return fmt.Errorf("unauthorized")
	}

	delete(s.accounts, id)
	return s.save()
}

// UpdateStatus updates an account's active status
func (s *Store) UpdateStatus(id string, isActive bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	acc, ok := s.accounts[id]
	if !ok {
		return fmt.Errorf("account not found")
	}

	acc.IsActive = isActive
	return s.save()
}

// Update updates an account in the store
func (s *Store) Update(account *Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.accounts[account.ID]; !ok {
		return fmt.Errorf("account not found")
	}

	s.accounts[account.ID] = account
	return s.save()
}

func (s *Store) load() error {
	filePath := filepath.Join(s.dataDir, "accounts.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var accounts []*Account
	if err := json.Unmarshal(data, &accounts); err != nil {
		return err
	}

	for _, acc := range accounts {
		s.accounts[acc.ID] = acc
	}

	return nil
}

func (s *Store) save() error {
	accounts := make([]*Account, 0, len(s.accounts))
	for _, acc := range s.accounts {
		accounts = append(accounts, acc)
	}

	data, err := json.MarshalIndent(accounts, "", "  ")
	if err != nil {
		return err
	}

	filePath := filepath.Join(s.dataDir, "accounts.json")
	return os.WriteFile(filePath, data, 0600)
}

func generateID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
