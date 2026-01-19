package contacts

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// CheckResult represents the result of checking phone numbers
type CheckResult struct {
	Valid   []*Contact `json:"valid"`   // Contacts that exist on Telegram
	Invalid []string   `json:"invalid"` // Phone numbers not registered on Telegram
	Retry   []string   `json:"retry"`   // Phone numbers that need retry (rate limited)
	Errors  []string   `json:"errors"`  // Any errors that occurred
}

// Checker handles phone number verification against Telegram
type Checker struct {
	store   *Store
	appID   int
	appHash string
}

// NewChecker creates a new phone number checker
func NewChecker(store *Store, appID int, appHash string) *Checker {
	return &Checker{
		store:   store,
		appID:   appID,
		appHash: appHash,
	}
}

// CheckNumbers verifies if phone numbers are registered on Telegram
// It uses the specified account's session to make the API calls
func (c *Checker) CheckNumbers(ctx context.Context, accountID string, sessionPath string, phones []string) (*CheckResult, error) {
	result := &CheckResult{
		Valid:   make([]*Contact, 0),
		Invalid: make([]string, 0),
		Retry:   make([]string, 0),
		Errors:  make([]string, 0),
	}

	if len(phones) == 0 {
		return result, nil
	}

	// Check if session file exists
	if _, err := os.Stat(sessionPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("session not found - please re-authenticate this account by removing and adding it again")
	}

	// Create session storage for this account
	sessionStorage := &telegram.FileSessionStorage{
		Path: sessionPath,
	}

	client := telegram.NewClient(c.appID, c.appHash, telegram.Options{
		SessionStorage: sessionStorage,
	})

	err := client.Run(ctx, func(ctx context.Context) error {
		// Get existing contacts to avoid deleting them later
		contactsResp, err := client.API().ContactsGetContacts(ctx, 0)
		if err != nil {
			// Check for auth errors
			if tgerr.Is(err, "AUTH_KEY_UNREGISTERED") || tgerr.Is(err, "SESSION_REVOKED") || tgerr.Is(err, "USER_DEACTIVATED") {
				return fmt.Errorf("session expired or revoked - please re-authenticate this account by removing and adding it again")
			}
			return fmt.Errorf("failed to get contacts: %w", err)
		}

		existingContacts := make(map[int64]bool)
		if contacts, ok := contactsResp.(*tg.ContactsContacts); ok {
			for _, u := range contacts.GetUsers() {
				existingContacts[u.GetID()] = true
			}
		}

		// Process phones in batches of 15 (Telegram limit)
		batchSize := 15
		for i := 0; i < len(phones); i += batchSize {
			end := i + batchSize
			if end > len(phones) {
				end = len(phones)
			}
			batch := phones[i:end]

			batchResult, err := c.checkBatch(ctx, client.API(), accountID, batch, existingContacts)
			if err != nil {
				slog.Error("batch check failed", "error", err, "batch_start", i)
				result.Errors = append(result.Errors, fmt.Sprintf("Batch %d failed: %s", i/batchSize+1, err.Error()))
				continue
			}

			result.Valid = append(result.Valid, batchResult.Valid...)
			result.Invalid = append(result.Invalid, batchResult.Invalid...)
			result.Retry = append(result.Retry, batchResult.Retry...)
		}

		return nil
	})

	if err != nil {
		// Check for auth errors and provide clear message
		errStr := err.Error()
		if tgerr.Is(err, "AUTH_KEY_UNREGISTERED") || tgerr.Is(err, "SESSION_REVOKED") || tgerr.Is(err, "USER_DEACTIVATED") {
			return nil, fmt.Errorf("session expired or revoked - please re-authenticate this account by removing and adding it again")
		}
		// Check if the error message contains auth-related text
		if strings.Contains(errStr, "AUTH_KEY_UNREGISTERED") || strings.Contains(errStr, "SESSION_REVOKED") {
			return nil, fmt.Errorf("session expired or revoked - please re-authenticate this account by removing and adding it again")
		}
		return nil, fmt.Errorf("telegram client error: %w", err)
	}

	// Save valid contacts to store
	if len(result.Valid) > 0 {
		if err := c.store.BulkCreateOrUpdate(result.Valid); err != nil {
			slog.Error("failed to save contacts", "error", err)
			result.Errors = append(result.Errors, "Failed to save some contacts")
		}
	}

	return result, nil
}

func (c *Checker) checkBatch(ctx context.Context, api *tg.Client, accountID string, phones []string, existingContacts map[int64]bool) (*CheckResult, error) {
	result := &CheckResult{
		Valid:   make([]*Contact, 0),
		Invalid: make([]string, 0),
		Retry:   make([]string, 0),
	}

	// Convert phones to input contacts
	inputContacts := make([]tg.InputPhoneContact, len(phones))
	for i, phone := range phones {
		inputContacts[i] = tg.InputPhoneContact{
			Phone:    phone,
			ClientID: int64(i),
		}
	}

	// Import contacts
	resp, err := c.importContactsWithRetry(ctx, api, inputContacts)
	if err != nil {
		return nil, err
	}

	// Track which phones were found
	foundPhones := make(map[string]bool)

	// Process imported contacts
	var toDelete []tg.InputUserClass
	for _, userClass := range resp.GetUsers() {
		user, ok := userClass.AsNotEmpty()
		if !ok {
			continue
		}

		foundPhones[user.Phone] = true

		contact := &Contact{
			AccountID:  accountID,
			TelegramID: user.ID,
			AccessHash: user.AccessHash,
			Phone:      user.Phone,
			FirstName:  user.FirstName,
			LastName:   user.LastName,
			Username:   user.Username,
			IsValid:    true,
		}
		result.Valid = append(result.Valid, contact)

		// Schedule for deletion if not in original contacts
		if !existingContacts[user.ID] {
			toDelete = append(toDelete, &tg.InputUser{
				UserID:     user.ID,
				AccessHash: user.AccessHash,
			})
		}
	}

	// Process retry contacts
	for _, retryIdx := range resp.GetRetryContacts() {
		if int(retryIdx) < len(phones) {
			result.Retry = append(result.Retry, phones[retryIdx])
		}
	}

	// Mark unfound phones as invalid
	for _, phone := range phones {
		if !foundPhones[phone] {
			// Check if it's not in retry list
			isRetry := false
			for _, r := range result.Retry {
				if r == phone {
					isRetry = true
					break
				}
			}
			if !isRetry {
				result.Invalid = append(result.Invalid, phone)
			}
		}
	}

	// Delete imported contacts that weren't in original contact list
	if len(toDelete) > 0 {
		if _, err := api.ContactsDeleteContacts(ctx, toDelete); err != nil {
			slog.Error("failed to delete contacts", "error", err)
			// Don't fail the whole operation for this
		}
	}

	return result, nil
}

func (c *Checker) importContactsWithRetry(ctx context.Context, api *tg.Client, contacts []tg.InputPhoneContact) (*tg.ContactsImportedContacts, error) {
	resp, err := api.ContactsImportContacts(ctx, contacts)
	if err == nil {
		return resp, nil
	}

	// Handle flood wait
	if flood, floodErr := tgerr.FloodWait(ctx, err); flood {
		slog.Info("flood wait, retrying...", "error", err)
		return c.importContactsWithRetry(ctx, api, contacts)
	} else if floodErr != nil {
		return nil, floodErr
	}

	return nil, err
}
