package contacts

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	tgclient "github.com/soluchok/tgsender/pkg/telegram"
)

// CheckResult represents the result of checking phone numbers
type CheckResult struct {
	Valid   []*Contact `json:"valid"`   // Contacts that exist on Telegram
	Invalid []string   `json:"invalid"` // Phone numbers/usernames not registered on Telegram
	Retry   []string   `json:"retry"`   // Phone numbers that need retry (rate limited)
	Errors  []string   `json:"errors"`  // Any errors that occurred
}

// CheckInput represents a mixed input of phones and usernames
type CheckInput struct {
	Phones    []string `json:"phones"`
	Usernames []string `json:"usernames"`
	Labels    []string `json:"labels"` // Custom labels to apply to contacts (if empty, auto-assigns based on input type)
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

// CheckContacts verifies if phones/usernames are registered on Telegram
// It uses the specified account's session to make the API calls
func (c *Checker) CheckContacts(ctx context.Context, accountID string, sessionPath string, proxyURL string, input *CheckInput) (*CheckResult, error) {
	result := &CheckResult{
		Valid:   make([]*Contact, 0),
		Invalid: make([]string, 0),
		Retry:   make([]string, 0),
		Errors:  make([]string, 0),
	}

	if len(input.Phones) == 0 && len(input.Usernames) == 0 {
		return result, nil
	}

	// Check if session file exists
	if _, err := os.Stat(sessionPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("session not found - please re-authenticate this account by removing and adding it again")
	}

	// Create Telegram client with optional proxy
	client, err := tgclient.CreateClient(c.appID, c.appHash, sessionPath, proxyURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram client: %w", err)
	}

	err = client.Run(ctx, func(ctx context.Context) error {
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

		// Process phones if any
		if len(input.Phones) > 0 {
			phoneResult, err := c.checkPhones(ctx, client.API(), accountID, input.Phones, existingContacts, input.Labels)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("Phone check failed: %s", err.Error()))
			} else {
				result.Valid = append(result.Valid, phoneResult.Valid...)
				result.Invalid = append(result.Invalid, phoneResult.Invalid...)
				result.Retry = append(result.Retry, phoneResult.Retry...)
			}
		}

		// Process usernames if any
		if len(input.Usernames) > 0 {
			usernameResult := c.resolveUsernames(ctx, client.API(), accountID, input.Usernames, input.Labels)
			result.Valid = append(result.Valid, usernameResult.Valid...)
			result.Invalid = append(result.Invalid, usernameResult.Invalid...)
			result.Errors = append(result.Errors, usernameResult.Errors...)
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

// checkPhones verifies phone numbers in batches
func (c *Checker) checkPhones(ctx context.Context, api *tg.Client, accountID string, phones []string, existingContacts map[int64]bool, labels []string) (*CheckResult, error) {
	result := &CheckResult{
		Valid:   make([]*Contact, 0),
		Invalid: make([]string, 0),
		Retry:   make([]string, 0),
		Errors:  make([]string, 0),
	}

	// Process phones in batches of 15 (Telegram limit)
	batchSize := 15
	for i := 0; i < len(phones); i += batchSize {
		end := i + batchSize
		if end > len(phones) {
			end = len(phones)
		}
		batch := phones[i:end]

		batchResult, err := c.checkBatch(ctx, api, accountID, batch, existingContacts, labels)
		if err != nil {
			slog.Error("batch check failed", "error", err, "batch_start", i)
			result.Errors = append(result.Errors, fmt.Sprintf("Batch %d failed: %s", i/batchSize+1, err.Error()))
			continue
		}

		result.Valid = append(result.Valid, batchResult.Valid...)
		result.Invalid = append(result.Invalid, batchResult.Invalid...)
		result.Retry = append(result.Retry, batchResult.Retry...)
	}

	return result, nil
}

// resolveUsernames resolves Telegram usernames to contacts
func (c *Checker) resolveUsernames(ctx context.Context, api *tg.Client, accountID string, usernames []string, labels []string) *CheckResult {
	result := &CheckResult{
		Valid:   make([]*Contact, 0),
		Invalid: make([]string, 0),
		Errors:  make([]string, 0),
	}

	for _, username := range usernames {
		// Remove @ prefix if present
		username = strings.TrimPrefix(username, "@")
		if username == "" {
			continue
		}

		resolved, err := c.resolveUsernameWithRetry(ctx, api, username)
		if err != nil {
			// Check if it's a "not found" error
			if tgerr.Is(err, "USERNAME_NOT_OCCUPIED") || tgerr.Is(err, "USERNAME_INVALID") {
				result.Invalid = append(result.Invalid, "@"+username)
				continue
			}
			slog.Error("failed to resolve username", "username", username, "error", err)
			result.Errors = append(result.Errors, fmt.Sprintf("@%s: %s", username, err.Error()))
			continue
		}

		// Extract user from resolved peer
		for _, userClass := range resolved.GetUsers() {
			user, ok := userClass.AsNotEmpty()
			if !ok {
				continue
			}

			// Only add if username matches (peer might be a channel/chat)
			if strings.EqualFold(user.Username, username) {
				photoURL := downloadUserPhoto(ctx, api, user)
				contact := &Contact{
					AccountID:  accountID,
					TelegramID: user.ID,
					AccessHash: user.AccessHash,
					Phone:      user.Phone,
					FirstName:  user.FirstName,
					LastName:   user.LastName,
					Username:   user.Username,
					PhotoURL:   photoURL,
					Labels:     labels,
					IsValid:    true,
				}
				result.Valid = append(result.Valid, contact)
				break
			}
		}

		// If no user was added, mark as invalid (might be a channel/chat)
		if len(result.Valid) == 0 || result.Valid[len(result.Valid)-1].Username != username {
			// Check if we already added this user
			found := false
			for _, v := range result.Valid {
				if strings.EqualFold(v.Username, username) {
					found = true
					break
				}
			}
			if !found {
				result.Invalid = append(result.Invalid, "@"+username)
			}
		}
	}

	return result
}

func (c *Checker) resolveUsernameWithRetry(ctx context.Context, api *tg.Client, username string) (*tg.ContactsResolvedPeer, error) {
	resolved, err := api.ContactsResolveUsername(ctx, username)
	if err == nil {
		return resolved, nil
	}

	// Handle flood wait
	if flood, floodErr := tgerr.FloodWait(ctx, err); flood {
		slog.Info("flood wait on resolve username, retrying...", "username", username)
		return c.resolveUsernameWithRetry(ctx, api, username)
	} else if floodErr != nil {
		return nil, floodErr
	}

	return nil, err
}

func (c *Checker) checkBatch(ctx context.Context, api *tg.Client, accountID string, phones []string, existingContacts map[int64]bool, labels []string) (*CheckResult, error) {
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

		photoURL := downloadUserPhoto(ctx, api, user)
		contact := &Contact{
			AccountID:  accountID,
			TelegramID: user.ID,
			AccessHash: user.AccessHash,
			Phone:      user.Phone,
			FirstName:  user.FirstName,
			LastName:   user.LastName,
			Username:   user.Username,
			PhotoURL:   photoURL,
			Labels:     labels,
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

// ChatContactsResult represents the result of importing contacts from chats
type ChatContactsResult struct {
	Imported int      `json:"imported"` // Number of contacts imported
	Skipped  int      `json:"skipped"`  // Number of contacts skipped (already exist or no access)
	Errors   []string `json:"errors"`   // Any errors that occurred
}

// ImportFromChats imports contacts from all dialogs (private chats) of the account
func (c *Checker) ImportFromChats(ctx context.Context, accountID string, sessionPath string, proxyURL string) (*ChatContactsResult, error) {
	result := &ChatContactsResult{
		Errors: make([]string, 0),
	}

	// Check if session file exists
	if _, err := os.Stat(sessionPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("session not found - please re-authenticate this account")
	}

	// Create Telegram client with optional proxy
	client, err := tgclient.CreateClient(c.appID, c.appHash, sessionPath, proxyURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram client: %w", err)
	}

	err = client.Run(ctx, func(ctx context.Context) error {
		// Get existing contacts from our store to check for duplicates
		existingContacts := make(map[int64]bool)
		for _, contact := range c.store.GetByAccount(accountID) {
			existingContacts[contact.TelegramID] = true
		}

		// Get all dialogs
		var allUsers []*tg.User
		var offsetDate int
		var offsetID int
		var offsetPeer tg.InputPeerClass = &tg.InputPeerEmpty{}

		for {
			resp, err := c.getDialogsWithRetry(ctx, client.API(), &tg.MessagesGetDialogsRequest{
				OffsetDate: offsetDate,
				OffsetID:   offsetID,
				OffsetPeer: offsetPeer,
				Limit:      100,
			})
			if err != nil {
				if tgerr.Is(err, "AUTH_KEY_UNREGISTERED") || tgerr.Is(err, "SESSION_REVOKED") {
					return fmt.Errorf("session expired - please re-authenticate")
				}
				return fmt.Errorf("failed to get dialogs: %w", err)
			}

			var dialogs []tg.DialogClass
			var users []tg.UserClass
			var messages []tg.MessageClass

			switch d := resp.(type) {
			case *tg.MessagesDialogs:
				dialogs = d.Dialogs
				users = d.Users
				messages = d.Messages
			case *tg.MessagesDialogsSlice:
				dialogs = d.Dialogs
				users = d.Users
				messages = d.Messages
			case *tg.MessagesDialogsNotModified:
				// No changes
				break
			}

			if len(dialogs) == 0 {
				break
			}

			// Build user map
			userMap := make(map[int64]*tg.User)
			for _, u := range users {
				if user, ok := u.AsNotEmpty(); ok {
					userMap[user.ID] = user
				}
			}

			// Extract users from private chats
			for _, dialog := range dialogs {
				d, ok := dialog.(*tg.Dialog)
				if !ok {
					continue
				}

				// Only process private chats (not groups/channels)
				if peerUser, ok := d.Peer.(*tg.PeerUser); ok {
					if user, exists := userMap[peerUser.UserID]; exists {
						// Skip bots and deleted accounts
						if user.Bot || user.Deleted {
							continue
						}
						allUsers = append(allUsers, user)
					}
				}
			}

			// Check if we got all dialogs
			if len(dialogs) < 100 {
				break
			}

			// Update offset for next iteration
			if len(messages) > 0 {
				lastMsg := messages[len(messages)-1]
				if msg, ok := lastMsg.(*tg.Message); ok {
					offsetDate = msg.Date
					offsetID = msg.ID
				}
			}
			if len(dialogs) > 0 {
				lastDialog := dialogs[len(dialogs)-1]
				if d, ok := lastDialog.(*tg.Dialog); ok {
					switch p := d.Peer.(type) {
					case *tg.PeerUser:
						if user, exists := userMap[p.UserID]; exists {
							offsetPeer = user.AsInputPeer()
						}
					}
				}
			}
		}

		// Import users as contacts
		var contactsToSave []*Contact
		for _, user := range allUsers {
			// Check if already exists - we'll still save to merge labels
			isExisting := existingContacts[user.ID]
			if isExisting {
				result.Skipped++
			}

			photoURL := downloadUserPhoto(ctx, client.API(), user)
			contact := &Contact{
				AccountID:  accountID,
				TelegramID: user.ID,
				AccessHash: user.AccessHash,
				Phone:      user.Phone,
				FirstName:  user.FirstName,
				LastName:   user.LastName,
				Username:   user.Username,
				PhotoURL:   photoURL,
				Labels:     []string{"chat"},
				IsValid:    true,
			}
			contactsToSave = append(contactsToSave, contact)
			existingContacts[user.ID] = true // Mark as seen to avoid duplicates in this batch
		}

		// Save contacts to store (BulkCreateOrUpdate will merge labels for duplicates)
		if len(contactsToSave) > 0 {
			if err := c.store.BulkCreateOrUpdate(contactsToSave); err != nil {
				slog.Error("failed to save contacts from chats", "error", err)
				result.Errors = append(result.Errors, "Failed to save some contacts")
			} else {
				result.Imported = len(contactsToSave) - result.Skipped
			}
		}

		return nil
	})

	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "AUTH_KEY_UNREGISTERED") || strings.Contains(errStr, "SESSION_REVOKED") {
			return nil, fmt.Errorf("session expired - please re-authenticate this account")
		}
		return nil, err
	}

	return result, nil
}

// ImportFromChatsWithProgress imports contacts from all dialogs with progress callback
func (c *Checker) ImportFromChatsWithProgress(ctx context.Context, accountID string, sessionPath string, proxyURL string, onProgress func(progress, imported, skipped int)) (*ChatContactsResult, error) {
	result := &ChatContactsResult{
		Errors: make([]string, 0),
	}

	// Check if session file exists
	if _, err := os.Stat(sessionPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("session not found - please re-authenticate this account")
	}

	// Create Telegram client with optional proxy
	client, err := tgclient.CreateClient(c.appID, c.appHash, sessionPath, proxyURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram client: %w", err)
	}

	err = client.Run(ctx, func(ctx context.Context) error {
		// Get existing contacts from our store to check for duplicates
		existingContacts := make(map[int64]bool)
		for _, contact := range c.store.GetByAccount(accountID) {
			existingContacts[contact.TelegramID] = true
		}

		// Get all dialogs
		var allUsers []*tg.User
		var offsetDate int
		var offsetID int
		var offsetPeer tg.InputPeerClass = &tg.InputPeerEmpty{}
		dialogsProcessed := 0
		seenDialogs := make(map[int64]bool) // Track seen dialog peer IDs to detect loops
		const batchLimit = 100

		for {
			resp, err := c.getDialogsWithRetry(ctx, client.API(), &tg.MessagesGetDialogsRequest{
				OffsetDate: offsetDate,
				OffsetID:   offsetID,
				OffsetPeer: offsetPeer,
				Limit:      batchLimit,
			})
			if err != nil {
				if tgerr.Is(err, "AUTH_KEY_UNREGISTERED") || tgerr.Is(err, "SESSION_REVOKED") {
					return fmt.Errorf("session expired - please re-authenticate")
				}
				return fmt.Errorf("failed to get dialogs: %w", err)
			}

			var dialogs []tg.DialogClass
			var users []tg.UserClass
			var messages []tg.MessageClass
			var chats []tg.ChatClass
			var isComplete bool

			switch d := resp.(type) {
			case *tg.MessagesDialogs:
				// All dialogs returned at once - no more pagination needed
				dialogs = d.Dialogs
				users = d.Users
				messages = d.Messages
				chats = d.Chats
				isComplete = true
			case *tg.MessagesDialogsSlice:
				dialogs = d.Dialogs
				users = d.Users
				messages = d.Messages
				chats = d.Chats
				// Check if we've fetched all dialogs
				isComplete = dialogsProcessed+len(dialogs) >= d.Count
			case *tg.MessagesDialogsNotModified:
				// No changes
				isComplete = true
			}

			if len(dialogs) == 0 {
				break
			}

			// Check for duplicate dialogs (pagination loop detection)
			// Only mark as seen AFTER processing, and break only if ALL dialogs were seen before
			allSeen := true
			for _, dialog := range dialogs {
				if d, ok := dialog.(*tg.Dialog); ok {
					peerID := getPeerID(d.Peer)
					if !seenDialogs[peerID] {
						allSeen = false
					}
				}
			}
			// If ALL dialogs were already seen, we're definitely in a loop
			if allSeen && len(dialogs) > 0 {
				slog.Info("pagination loop detected - all dialogs already seen", "count", len(dialogs))
				break
			}

			// Mark dialogs as seen
			for _, dialog := range dialogs {
				if d, ok := dialog.(*tg.Dialog); ok {
					peerID := getPeerID(d.Peer)
					seenDialogs[peerID] = true
				}
			}

			dialogsProcessed += len(dialogs)

			// Build user map
			userMap := make(map[int64]*tg.User)
			for _, u := range users {
				if user, ok := u.AsNotEmpty(); ok {
					userMap[user.ID] = user
				}
			}

			// Build chat/channel maps for offset peer resolution
			chatMap := make(map[int64]*tg.Chat)
			channelMap := make(map[int64]*tg.Channel)
			for _, chatClass := range chats {
				switch chat := chatClass.(type) {
				case *tg.Chat:
					chatMap[chat.ID] = chat
				case *tg.Channel:
					channelMap[chat.ID] = chat
				}
			}

			// Extract users from private chats
			for _, dialog := range dialogs {
				d, ok := dialog.(*tg.Dialog)
				if !ok {
					continue
				}

				// Only process private chats (not groups/channels)
				if peerUser, ok := d.Peer.(*tg.PeerUser); ok {
					if user, exists := userMap[peerUser.UserID]; exists {
						// Skip bots and deleted accounts
						if user.Bot || user.Deleted {
							continue
						}
						allUsers = append(allUsers, user)
					}
				}
			}

			// Calculate current imported/skipped for progress
			currentImported := 0
			currentSkipped := 0
			for _, user := range allUsers {
				if existingContacts[user.ID] {
					currentSkipped++
				} else {
					currentImported++
				}
			}

			// Report progress after each batch
			if onProgress != nil {
				onProgress(dialogsProcessed, currentImported, currentSkipped)
			}

			// Check if we've got all dialogs
			if isComplete || len(dialogs) < batchLimit {
				break
			}

			// Update offset for next iteration using the last dialog
			lastDialog := dialogs[len(dialogs)-1]
			if d, ok := lastDialog.(*tg.Dialog); ok {
				// Build a map of messages by their peer for easier lookup
				// Messages in the response correspond to the top message of each dialog
				messageMap := make(map[int]tg.MessageClass)
				for _, msgClass := range messages {
					switch msg := msgClass.(type) {
					case *tg.Message:
						messageMap[msg.ID] = msg
					case *tg.MessageService:
						messageMap[msg.ID] = msg
					}
				}

				// Find the message for the last dialog's top message
				if msgClass, exists := messageMap[d.TopMessage]; exists {
					switch msg := msgClass.(type) {
					case *tg.Message:
						offsetDate = msg.Date
						offsetID = msg.ID
					case *tg.MessageService:
						offsetDate = msg.Date
						offsetID = msg.ID
					}
				} else {
					// Fallback: use the last message in the list
					if len(messages) > 0 {
						switch msg := messages[len(messages)-1].(type) {
						case *tg.Message:
							offsetDate = msg.Date
							offsetID = msg.ID
						case *tg.MessageService:
							offsetDate = msg.Date
							offsetID = msg.ID
						}
					}
				}

				// Set offset peer based on dialog peer type
				switch p := d.Peer.(type) {
				case *tg.PeerUser:
					if user, exists := userMap[p.UserID]; exists {
						offsetPeer = user.AsInputPeer()
					}
				case *tg.PeerChat:
					if chat, exists := chatMap[p.ChatID]; exists {
						offsetPeer = chat.AsInputPeer()
					}
				case *tg.PeerChannel:
					if channel, exists := channelMap[p.ChannelID]; exists {
						offsetPeer = channel.AsInputPeer()
					}
				}
			}
		}

		// Import users as contacts (download photos while we still have the client)
		var contactsToSave []*Contact
		for _, user := range allUsers {
			// Check if already exists - we'll still save to merge labels
			isExisting := existingContacts[user.ID]
			if isExisting {
				result.Skipped++
			}

			// Download profile photo
			photoURL := downloadUserPhoto(ctx, client.API(), user)

			contact := &Contact{
				AccountID:  accountID,
				TelegramID: user.ID,
				AccessHash: user.AccessHash,
				Phone:      user.Phone,
				FirstName:  user.FirstName,
				LastName:   user.LastName,
				Username:   user.Username,
				PhotoURL:   photoURL,
				Labels:     []string{"chat"},
				IsValid:    true,
			}
			contactsToSave = append(contactsToSave, contact)
			existingContacts[user.ID] = true // Mark as seen to avoid duplicates in this batch
		}

		// Save contacts to store (BulkCreateOrUpdate will merge labels for duplicates)
		if len(contactsToSave) > 0 {
			if err := c.store.BulkCreateOrUpdate(contactsToSave); err != nil {
				slog.Error("failed to save contacts from chats", "error", err)
				result.Errors = append(result.Errors, "Failed to save some contacts")
			} else {
				result.Imported = len(contactsToSave) - result.Skipped
			}
		}

		return nil
	})

	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "AUTH_KEY_UNREGISTERED") || strings.Contains(errStr, "SESSION_REVOKED") {
			return nil, fmt.Errorf("session expired - please re-authenticate this account")
		}
		return nil, err
	}

	return result, nil
}

// ImportFromContacts imports contacts from Telegram's contact list
func (c *Checker) ImportFromContacts(ctx context.Context, accountID string, sessionPath string, proxyURL string, onProgress func(imported, skipped int)) (*ChatContactsResult, error) {
	result := &ChatContactsResult{
		Errors: make([]string, 0),
	}

	// Check if session file exists
	if _, err := os.Stat(sessionPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("session not found - please re-authenticate this account")
	}

	// Create Telegram client with optional proxy
	client, err := tgclient.CreateClient(c.appID, c.appHash, sessionPath, proxyURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram client: %w", err)
	}

	err = client.Run(ctx, func(ctx context.Context) error {
		// Get existing contacts from our store to check for duplicates
		existingContacts := make(map[int64]bool)
		for _, contact := range c.store.GetByAccount(accountID) {
			existingContacts[contact.TelegramID] = true
		}

		// Get contacts from Telegram
		resp, err := c.getContactsWithRetry(ctx, client.API())
		if err != nil {
			if tgerr.Is(err, "AUTH_KEY_UNREGISTERED") || tgerr.Is(err, "SESSION_REVOKED") {
				return fmt.Errorf("session expired - please re-authenticate")
			}
			return fmt.Errorf("failed to get contacts: %w", err)
		}

		contacts, ok := resp.(*tg.ContactsContacts)
		if !ok {
			// ContactsContactsNotModified means no contacts
			return nil
		}

		// Build user map
		userMap := make(map[int64]*tg.User)
		for _, u := range contacts.Users {
			if user, ok := u.AsNotEmpty(); ok {
				userMap[user.ID] = user
			}
		}

		// Import contacts
		var contactsToSave []*Contact
		for _, tgContact := range contacts.Contacts {
			user, exists := userMap[tgContact.UserID]
			if !exists {
				continue
			}

			// Skip bots and deleted accounts
			if user.Bot || user.Deleted {
				continue
			}

			// Check if already exists - we'll still save to merge labels
			isExisting := existingContacts[user.ID]
			if isExisting {
				result.Skipped++
			}

			// Download profile photo
			photoURL := downloadUserPhoto(ctx, client.API(), user)

			contact := &Contact{
				AccountID:  accountID,
				TelegramID: user.ID,
				AccessHash: user.AccessHash,
				Phone:      user.Phone,
				FirstName:  user.FirstName,
				LastName:   user.LastName,
				Username:   user.Username,
				PhotoURL:   photoURL,
				Labels:     []string{"contact"},
				IsValid:    true,
			}
			contactsToSave = append(contactsToSave, contact)
			existingContacts[user.ID] = true

			if onProgress != nil {
				onProgress(len(contactsToSave)-result.Skipped, result.Skipped)
			}
		}

		// Save contacts to store (BulkCreateOrUpdate will merge labels for duplicates)
		if len(contactsToSave) > 0 {
			if err := c.store.BulkCreateOrUpdate(contactsToSave); err != nil {
				slog.Error("failed to save contacts", "error", err)
				result.Errors = append(result.Errors, "Failed to save some contacts")
			} else {
				result.Imported = len(contactsToSave) - result.Skipped
			}
		}

		return nil
	})

	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "AUTH_KEY_UNREGISTERED") || strings.Contains(errStr, "SESSION_REVOKED") {
			return nil, fmt.Errorf("session expired - please re-authenticate this account")
		}
		return nil, err
	}

	return result, nil
}

func (c *Checker) getContactsWithRetry(ctx context.Context, api *tg.Client) (tg.ContactsContactsClass, error) {
	resp, err := api.ContactsGetContacts(ctx, 0)
	if err == nil {
		return resp, nil
	}

	// Handle flood wait
	if flood, floodErr := tgerr.FloodWait(ctx, err); flood {
		slog.Info("flood wait on get contacts, retrying...", "error", err)
		return c.getContactsWithRetry(ctx, api)
	} else if floodErr != nil {
		return nil, floodErr
	}

	return nil, err
}

func (c *Checker) getDialogsWithRetry(ctx context.Context, api *tg.Client, req *tg.MessagesGetDialogsRequest) (tg.MessagesDialogsClass, error) {
	resp, err := api.MessagesGetDialogs(ctx, req)
	if err == nil {
		return resp, nil
	}

	// Handle flood wait
	if flood, floodErr := tgerr.FloodWait(ctx, err); flood {
		slog.Info("flood wait on get dialogs, retrying...", "error", err)
		return c.getDialogsWithRetry(ctx, api, req)
	} else if floodErr != nil {
		return nil, floodErr
	}

	return nil, err
}

// getPeerID extracts the ID from a peer
func getPeerID(peer tg.PeerClass) int64 {
	switch p := peer.(type) {
	case *tg.PeerUser:
		return p.UserID
	case *tg.PeerChat:
		return p.ChatID
	case *tg.PeerChannel:
		return p.ChannelID
	}
	return 0
}

// FileImportResult represents the result of importing contacts from a file
type FileImportResult struct {
	Imported int      `json:"imported"` // Number of contacts successfully imported
	Skipped  int      `json:"skipped"`  // Number of contacts skipped (already exist)
	Failed   int      `json:"failed"`   // Number of contacts that failed to resolve
	Errors   []string `json:"errors"`   // Detailed errors for failed contacts
}

// FlexInt64 is an int64 that can unmarshal from either a JSON number or string
type FlexInt64 int64

func (f *FlexInt64) UnmarshalJSON(data []byte) error {
	// Remove quotes if present (string representation)
	s := strings.Trim(string(data), "\"")
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return err
	}
	*f = FlexInt64(val)
	return nil
}

// FileImportContact represents a contact to be imported from a file
type FileImportContact struct {
	TelegramID int64     `json:"telegram_id,string"`
	AccessHash FlexInt64 `json:"access_hash,omitempty"` // If from same account, can reuse access_hash
	AccountID  string    `json:"account_id,omitempty"`  // Original account ID this contact was exported from
	Phone      string    `json:"phone"`
	FirstName  string    `json:"first_name"`
	LastName   string    `json:"last_name,omitempty"`
	Username   string    `json:"username,omitempty"`
	PhotoURL   string    `json:"photo_url,omitempty"` // Base64 encoded profile photo
	Labels     []string  `json:"labels,omitempty"`
}

// ImportFromFile imports contacts from a previously exported file
// It resolves contacts by phone or username to get valid access_hash for the importing account
func (c *Checker) ImportFromFile(ctx context.Context, accountID string, sessionPath string, proxyURL string, importContacts []FileImportContact) (*FileImportResult, error) {
	result := &FileImportResult{
		Errors: make([]string, 0),
	}

	if len(importContacts) == 0 {
		return result, nil
	}

	// Check if session file exists
	if _, err := os.Stat(sessionPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("session not found - please re-authenticate this account")
	}

	// Create Telegram client with optional proxy
	client, err := tgclient.CreateClient(c.appID, c.appHash, sessionPath, proxyURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram client: %w", err)
	}

	err = client.Run(ctx, func(ctx context.Context) error {
		// Get existing contacts from our store
		existingContacts := make(map[int64]*Contact)
		for _, contact := range c.store.GetByAccount(accountID) {
			existingContacts[contact.TelegramID] = contact
		}

		// Get existing Telegram contacts to avoid deleting them later
		contactsResp, err := client.API().ContactsGetContacts(ctx, 0)
		if err != nil {
			if tgerr.Is(err, "AUTH_KEY_UNREGISTERED") || tgerr.Is(err, "SESSION_REVOKED") {
				return fmt.Errorf("session expired - please re-authenticate")
			}
			return fmt.Errorf("failed to get contacts: %w", err)
		}

		existingTgContacts := make(map[int64]bool)
		if contacts, ok := contactsResp.(*tg.ContactsContacts); ok {
			for _, u := range contacts.GetUsers() {
				existingTgContacts[u.GetID()] = true
			}
		}

		var contactsToSave []*Contact

		// Separate contacts by resolution method
		var phonesToResolve []string
		var usernamesToResolve []string
		phoneToImport := make(map[string]FileImportContact)
		usernameToImport := make(map[string]FileImportContact)

		for _, ic := range importContacts {
			// Check if contact already exists in our store with valid data
			if existing, ok := existingContacts[ic.TelegramID]; ok && existing.AccessHash != 0 {
				// Contact already exists, merge labels
				existing.Labels = mergeLabels(existing.Labels, ic.Labels)
				contactsToSave = append(contactsToSave, existing)
				result.Skipped++
				continue
			}

			// If contact was exported from the same account and has valid access_hash, reuse it
			// This works because access_hash is account-specific in Telegram
			if ic.AccountID == accountID && ic.AccessHash != 0 && ic.TelegramID != 0 {
				contactID, err := generateID()
				if err != nil {
					result.Failed++
					result.Errors = append(result.Errors, fmt.Sprintf("Failed to generate ID for '%s %s': %v", ic.FirstName, ic.LastName, err))
					continue
				}
				contact := &Contact{
					ID:         contactID,
					AccountID:  accountID,
					TelegramID: ic.TelegramID,
					AccessHash: int64(ic.AccessHash),
					Phone:      ic.Phone,
					FirstName:  ic.FirstName,
					LastName:   ic.LastName,
					Username:   ic.Username,
					PhotoURL:   ic.PhotoURL,
					Labels:     ic.Labels,
					IsValid:    true,
					CreatedAt:  time.Now(),
					UpdatedAt:  time.Now(),
				}
				contactsToSave = append(contactsToSave, contact)
				result.Imported++
				continue
			}

			// Need to resolve this contact
			// Prefer phone resolution, fallback to username
			if ic.Phone != "" {
				phonesToResolve = append(phonesToResolve, ic.Phone)
				phoneToImport[ic.Phone] = ic
			} else if ic.Username != "" {
				username := strings.TrimPrefix(ic.Username, "@")
				usernamesToResolve = append(usernamesToResolve, username)
				usernameToImport[strings.ToLower(username)] = ic
			} else {
				// No phone or username, can't resolve
				result.Failed++
				result.Errors = append(result.Errors, fmt.Sprintf("Contact '%s %s' has no phone or username", ic.FirstName, ic.LastName))
			}
		}

		// Resolve phones in batches
		if len(phonesToResolve) > 0 {
			batchSize := 15
			for i := 0; i < len(phonesToResolve); i += batchSize {
				end := i + batchSize
				if end > len(phonesToResolve) {
					end = len(phonesToResolve)
				}
				batch := phonesToResolve[i:end]

				// Convert phones to input contacts
				inputContacts := make([]tg.InputPhoneContact, len(batch))
				for j, phone := range batch {
					inputContacts[j] = tg.InputPhoneContact{
						Phone:    phone,
						ClientID: int64(j),
					}
				}

				resp, err := c.importContactsWithRetry(ctx, client.API(), inputContacts)
				if err != nil {
					slog.Error("batch import failed", "error", err)
					for _, phone := range batch {
						ic := phoneToImport[phone]
						result.Failed++
						result.Errors = append(result.Errors, fmt.Sprintf("Failed to resolve '%s %s' by phone: %s", ic.FirstName, ic.LastName, err.Error()))
					}
					continue
				}

				// Track found phones
				foundPhones := make(map[string]bool)
				var toDelete []tg.InputUserClass

				for _, userClass := range resp.GetUsers() {
					user, ok := userClass.AsNotEmpty()
					if !ok {
						continue
					}

					foundPhones[user.Phone] = true
					ic := phoneToImport[user.Phone]

					photoURL := downloadUserPhoto(ctx, client.API(), user)
					contact := &Contact{
						AccountID:  accountID,
						TelegramID: user.ID,
						AccessHash: user.AccessHash,
						Phone:      user.Phone,
						FirstName:  ic.FirstName, // Preserve original name from file
						LastName:   ic.LastName,
						Username:   user.Username, // Use current username from Telegram
						PhotoURL:   photoURL,
						Labels:     ic.Labels,
						IsValid:    true,
					}
					// Use Telegram names if file names are empty
					if contact.FirstName == "" {
						contact.FirstName = user.FirstName
					}
					if contact.LastName == "" {
						contact.LastName = user.LastName
					}
					contactsToSave = append(contactsToSave, contact)
					result.Imported++

					// Schedule for deletion if not in original Telegram contacts
					if !existingTgContacts[user.ID] {
						toDelete = append(toDelete, &tg.InputUser{
							UserID:     user.ID,
							AccessHash: user.AccessHash,
						})
					}
				}

				// Delete imported contacts that weren't in original contact list
				if len(toDelete) > 0 {
					if _, err := client.API().ContactsDeleteContacts(ctx, toDelete); err != nil {
						slog.Debug("failed to delete contacts", "error", err)
					}
				}

				// Mark not found phones for username resolution
				for _, phone := range batch {
					if !foundPhones[phone] {
						ic := phoneToImport[phone]
						// Try username if available
						if ic.Username != "" {
							username := strings.TrimPrefix(ic.Username, "@")
							usernamesToResolve = append(usernamesToResolve, username)
							usernameToImport[strings.ToLower(username)] = ic
						} else {
							result.Failed++
							result.Errors = append(result.Errors, fmt.Sprintf("Phone %s not registered on Telegram (%s %s)", phone, ic.FirstName, ic.LastName))
						}
					}
				}
			}
		}

		// Resolve usernames
		for _, username := range usernamesToResolve {
			ic := usernameToImport[strings.ToLower(username)]

			resolved, err := c.resolveUsernameWithRetry(ctx, client.API(), username)
			if err != nil {
				if tgerr.Is(err, "USERNAME_NOT_OCCUPIED") || tgerr.Is(err, "USERNAME_INVALID") {
					result.Failed++
					result.Errors = append(result.Errors, fmt.Sprintf("Username @%s not found (%s %s)", username, ic.FirstName, ic.LastName))
					continue
				}
				result.Failed++
				result.Errors = append(result.Errors, fmt.Sprintf("Failed to resolve @%s: %s", username, err.Error()))
				continue
			}

			// Extract user from resolved peer
			var found bool
			for _, userClass := range resolved.GetUsers() {
				user, ok := userClass.AsNotEmpty()
				if !ok {
					continue
				}

				if strings.EqualFold(user.Username, username) {
					photoURL := downloadUserPhoto(ctx, client.API(), user)
					contact := &Contact{
						AccountID:  accountID,
						TelegramID: user.ID,
						AccessHash: user.AccessHash,
						Phone:      user.Phone,
						FirstName:  ic.FirstName,
						LastName:   ic.LastName,
						Username:   user.Username,
						PhotoURL:   photoURL,
						Labels:     ic.Labels,
						IsValid:    true,
					}
					// Use Telegram names if file names are empty
					if contact.FirstName == "" {
						contact.FirstName = user.FirstName
					}
					if contact.LastName == "" {
						contact.LastName = user.LastName
					}
					contactsToSave = append(contactsToSave, contact)
					result.Imported++
					found = true
					break
				}
			}

			if !found {
				result.Failed++
				result.Errors = append(result.Errors, fmt.Sprintf("Username @%s resolved but user not found (%s %s)", username, ic.FirstName, ic.LastName))
			}
		}

		// Save all resolved contacts
		if len(contactsToSave) > 0 {
			if err := c.store.BulkCreateOrUpdate(contactsToSave); err != nil {
				slog.Error("failed to save imported contacts", "error", err)
				return fmt.Errorf("failed to save contacts: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "AUTH_KEY_UNREGISTERED") || strings.Contains(errStr, "SESSION_REVOKED") {
			return nil, fmt.Errorf("session expired - please re-authenticate this account")
		}
		return nil, err
	}

	return result, nil
}

// downloadUserPhoto downloads the profile photo for a user and returns base64 encoded data URL
func downloadUserPhoto(ctx context.Context, api *tg.Client, user *tg.User) string {
	if user.Photo == nil {
		return "" // No photo set
	}

	photo, ok := user.Photo.AsNotEmpty()
	if !ok {
		return "" // No photo set
	}

	d := downloader.NewDownloader()
	var buf strings.Builder
	writer := base64.NewEncoder(base64.StdEncoding, &buf)

	_, err := d.Download(api, &tg.InputPeerPhotoFileLocation{
		Peer:    user.AsInputPeer(),
		PhotoID: photo.PhotoID,
	}).Stream(ctx, writer)
	writer.Close()

	if err != nil {
		slog.Debug("failed to download user photo", "userID", user.ID, "error", err)
		return ""
	}

	return "data:image/jpeg;base64," + buf.String()
}
