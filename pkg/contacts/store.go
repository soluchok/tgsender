package contacts

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Contact represents a verified Telegram contact
type Contact struct {
	ID         string    `json:"id"`
	AccountID  string    `json:"account_id"`         // The account that verified this contact
	TelegramID int64     `json:"telegram_id,string"` // Telegram user ID of the contact
	AccessHash int64     `json:"access_hash,string"` // Required for sending messages
	Phone      string    `json:"phone"`
	FirstName  string    `json:"first_name"`
	LastName   string    `json:"last_name,omitempty"`
	Username   string    `json:"username,omitempty"`
	PhotoURL   string    `json:"photo_url,omitempty"` // Base64 encoded profile photo
	Labels     []string  `json:"labels,omitempty"`    // Tags/labels for the contact (e.g., "chat", "phone", "username")
	IsValid    bool      `json:"is_valid"`            // Whether the phone is registered on Telegram
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Store manages contact storage
type Store struct {
	mu       sync.RWMutex
	dataDir  string
	contacts map[string]*Contact // keyed by contact ID
}

// NewStore creates a new contact store
func NewStore(dataDir string) (*Store, error) {
	store := &Store{
		dataDir:  dataDir,
		contacts: make(map[string]*Contact),
	}

	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	if err := store.load(); err != nil {
		return nil, fmt.Errorf("failed to load contacts: %w", err)
	}

	return store, nil
}

// GetByAccount returns all contacts for a specific account
func (s *Store) GetByAccount(accountID string) []*Contact {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var contacts []*Contact
	for _, c := range s.contacts {
		if c.AccountID == accountID {
			contacts = append(contacts, c)
		}
	}
	return contacts
}

// GetValidByAccount returns only valid contacts for a specific account
func (s *Store) GetValidByAccount(accountID string) []*Contact {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var contacts []*Contact
	for _, c := range s.contacts {
		if c.AccountID == accountID && c.IsValid {
			contacts = append(contacts, c)
		}
	}
	return contacts
}

// Get returns a contact by ID
func (s *Store) Get(id string) (*Contact, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	c, ok := s.contacts[id]
	return c, ok
}

// GetByPhone returns a contact by account ID and phone number
func (s *Store) GetByPhone(accountID, phone string) (*Contact, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, c := range s.contacts {
		if c.AccountID == accountID && c.Phone == phone {
			return c, true
		}
	}
	return nil, false
}

// CreateOrUpdate adds a new contact or updates an existing one
func (s *Store) CreateOrUpdate(contact *Contact) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for existing contact by account ID and phone
	for _, existing := range s.contacts {
		if existing.AccountID == contact.AccountID && existing.Phone == contact.Phone {
			// Update existing contact
			contact.ID = existing.ID
			contact.CreatedAt = existing.CreatedAt
			contact.UpdatedAt = time.Now()
			s.contacts[contact.ID] = contact
			return s.save()
		}
	}

	// Create new contact
	if contact.ID == "" {
		id, err := generateID()
		if err != nil {
			return err
		}
		contact.ID = id
	}

	contact.CreatedAt = time.Now()
	contact.UpdatedAt = time.Now()
	s.contacts[contact.ID] = contact

	return s.save()
}

// BulkCreateOrUpdate adds multiple contacts efficiently
// For existing contacts, it merges labels and preserves non-empty names
func (s *Store) BulkCreateOrUpdate(contacts []*Contact) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, contact := range contacts {
		// Check for existing contact by account ID and TelegramID
		var found bool
		for _, existing := range s.contacts {
			if existing.AccountID == contact.AccountID && existing.TelegramID == contact.TelegramID {
				// Update existing contact
				contact.ID = existing.ID
				contact.CreatedAt = existing.CreatedAt
				contact.UpdatedAt = time.Now()
				// Merge labels
				contact.Labels = mergeLabels(existing.Labels, contact.Labels)
				// Keep existing names if new ones are empty
				if contact.FirstName == "" {
					contact.FirstName = existing.FirstName
				}
				if contact.LastName == "" {
					contact.LastName = existing.LastName
				}
				s.contacts[contact.ID] = contact
				found = true
				break
			}
		}

		if !found {
			// Create new contact
			if contact.ID == "" {
				id, err := generateID()
				if err != nil {
					return err
				}
				contact.ID = id
			}
			contact.CreatedAt = time.Now()
			contact.UpdatedAt = time.Now()
			s.contacts[contact.ID] = contact
		}
	}

	return s.save()
}

// mergeLabels combines two label slices, removing duplicates
func mergeLabels(existing, new []string) []string {
	labelSet := make(map[string]bool)
	for _, l := range existing {
		labelSet[l] = true
	}
	for _, l := range new {
		labelSet[l] = true
	}

	merged := make([]string, 0, len(labelSet))
	for l := range labelSet {
		merged = append(merged, l)
	}
	return merged
}

// Delete removes a contact
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.contacts[id]; !ok {
		return fmt.Errorf("contact not found")
	}

	delete(s.contacts, id)
	return s.save()
}

// Update updates a contact's editable fields (first name, last name, labels)
func (s *Store) Update(id string, firstName, lastName string, labels []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	contact, ok := s.contacts[id]
	if !ok {
		return fmt.Errorf("contact not found")
	}

	contact.FirstName = firstName
	contact.LastName = lastName
	contact.Labels = labels
	contact.UpdatedAt = time.Now()

	return s.save()
}

// DeleteByAccount removes all contacts for a specific account
func (s *Store) DeleteByAccount(accountID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, c := range s.contacts {
		if c.AccountID == accountID {
			delete(s.contacts, id)
		}
	}
	return s.save()
}

func (s *Store) load() error {
	filePath := filepath.Join(s.dataDir, "contacts.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var contacts []*Contact
	if err := json.Unmarshal(data, &contacts); err != nil {
		return err
	}

	for _, c := range contacts {
		s.contacts[c.ID] = c
	}

	return nil
}

func (s *Store) save() error {
	contacts := make([]*Contact, 0, len(s.contacts))
	for _, c := range s.contacts {
		contacts = append(contacts, c)
	}

	data, err := json.MarshalIndent(contacts, "", "  ")
	if err != nil {
		return err
	}

	filePath := filepath.Join(s.dataDir, "contacts.json")
	return os.WriteFile(filePath, data, 0600)
}

func generateID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
