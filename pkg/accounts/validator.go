package accounts

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
)

// Validator checks if Telegram sessions are still valid
type Validator struct {
	store   *Store
	appID   int
	appHash string
}

// NewValidator creates a new session validator
func NewValidator(store *Store, appID int, appHash string) *Validator {
	return &Validator{
		store:   store,
		appID:   appID,
		appHash: appHash,
	}
}

// ValidationResult contains the result of session validation
type ValidationResult struct {
	IsValid  bool
	PhotoURL string
}

// ValidateSession checks if an account's Telegram session is still valid
// and fetches the profile photo if available
func (v *Validator) ValidateSession(ctx context.Context, account *Account) (*ValidationResult, error) {
	result := &ValidationResult{}

	if account.SessionToken == "" {
		return result, nil
	}

	sessionPath := ".data/account_" + account.SessionToken + ".json"

	// Check if session file exists
	if _, err := os.Stat(sessionPath); os.IsNotExist(err) {
		return result, nil
	}

	sessionStorage := &telegram.FileSessionStorage{
		Path: sessionPath,
	}

	client := telegram.NewClient(v.appID, v.appHash, telegram.Options{
		SessionStorage: sessionStorage,
	})

	err := client.Run(ctx, func(ctx context.Context) error {
		// Try to get self - if this succeeds, session is valid
		self, err := client.Self(ctx)
		if err != nil {
			errStr := err.Error()
			if strings.Contains(errStr, "AUTH_KEY_UNREGISTERED") ||
				strings.Contains(errStr, "SESSION_REVOKED") ||
				strings.Contains(errStr, "USER_DEACTIVATED") {
				return nil // Session invalid, but not an error
			}
			return err
		}
		result.IsValid = true

		// Try to get profile photo
		photo, ok := self.Photo.AsNotEmpty()
		if !ok {
			return nil // No photo set
		}

		// Download the photo
		d := downloader.NewDownloader()
		var buf strings.Builder
		writer := base64.NewEncoder(base64.StdEncoding, &buf)

		_, err = d.Download(client.API(), &tg.InputPeerPhotoFileLocation{
			Peer:    self.AsInputPeer(),
			PhotoID: photo.PhotoID,
		}).Stream(ctx, writer)
		writer.Close()

		if err != nil {
			// Photo download failed, but session is still valid
			return nil
		}

		result.PhotoURL = "data:image/jpeg;base64," + buf.String()
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to validate session: %w", err)
	}

	return result, nil
}

// ValidateAndUpdateStatus validates a session and updates the account's status and photo
func (v *Validator) ValidateAndUpdateStatus(ctx context.Context, accountID string) (*ValidationResult, error) {
	account, ok := v.store.Get(accountID)
	if !ok {
		return nil, fmt.Errorf("account not found")
	}

	result, err := v.ValidateSession(ctx, account)
	if err != nil {
		return nil, err
	}

	// Update account if anything changed
	needsUpdate := false
	if account.IsActive != result.IsValid {
		account.IsActive = result.IsValid
		needsUpdate = true
	}
	if result.PhotoURL != "" && account.PhotoURL != result.PhotoURL {
		account.PhotoURL = result.PhotoURL
		needsUpdate = true
	}

	if needsUpdate {
		if err := v.store.Update(account); err != nil {
			return result, fmt.Errorf("failed to update account: %w", err)
		}
	}

	return result, nil
}
