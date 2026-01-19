package messages

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"github.com/soluchok/tgsender/pkg/contacts"
)

// SendResult represents the result of sending messages
type SendResult struct {
	Total      int               `json:"total"`
	Successful int               `json:"successful"`
	Failed     int               `json:"failed"`
	Results    []RecipientResult `json:"results"`
}

// RecipientResult represents the result for a single recipient
type RecipientResult struct {
	ContactID string `json:"contact_id"`
	Phone     string `json:"phone"`
	Name      string `json:"name"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
}

// Sender handles sending messages via Telegram
type Sender struct {
	contactStore *contacts.Store
	appID        int
	appHash      string
}

// NewSender creates a new message sender
func NewSender(contactStore *contacts.Store, appID int, appHash string) *Sender {
	return &Sender{
		contactStore: contactStore,
		appID:        appID,
		appHash:      appHash,
	}
}

// SendToContacts sends a message to the specified contacts
func (s *Sender) SendToContacts(ctx context.Context, sessionPath string, contactIDs []string, messageText string) (*SendResult, error) {
	result := &SendResult{
		Results: make([]RecipientResult, 0),
	}

	if len(contactIDs) == 0 {
		return result, nil
	}

	if messageText == "" {
		return nil, fmt.Errorf("message text is required")
	}

	// Check if session file exists
	if _, err := os.Stat(sessionPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("session not found - please re-authenticate this account")
	}

	// Get contacts by IDs
	var contactsToSend []*contacts.Contact
	for _, id := range contactIDs {
		contact, ok := s.contactStore.Get(id)
		if ok && contact.IsValid {
			contactsToSend = append(contactsToSend, contact)
		}
	}

	if len(contactsToSend) == 0 {
		return nil, fmt.Errorf("no valid contacts found")
	}

	result.Total = len(contactsToSend)

	// Create session storage
	sessionStorage := &telegram.FileSessionStorage{
		Path: sessionPath,
	}

	client := telegram.NewClient(s.appID, s.appHash, telegram.Options{
		SessionStorage: sessionStorage,
	})

	err := client.Run(ctx, func(ctx context.Context) error {
		sender := message.NewSender(client.API())

		// Track already sent to avoid duplicates
		sent := make(map[int64]bool)

		for _, contact := range contactsToSend {
			recipientResult := RecipientResult{
				ContactID: contact.ID,
				Phone:     contact.Phone,
				Name:      formatName(contact.FirstName, contact.LastName),
			}

			// Skip if already sent to this telegram ID
			if sent[contact.TelegramID] {
				recipientResult.Success = true
				recipientResult.Error = "duplicate, skipped"
				result.Results = append(result.Results, recipientResult)
				continue
			}

			sent[contact.TelegramID] = true

			// Create peer
			peer := &tg.InputPeerUser{
				UserID:     contact.TelegramID,
				AccessHash: contact.AccessHash,
			}

			// Send message
			err := sendMessage(ctx, sender, peer, messageText, contact.Username)
			if err != nil {
				recipientResult.Success = false
				recipientResult.Error = err.Error()
				result.Failed++
				slog.Error("failed to send message",
					slog.Int64("telegram_id", contact.TelegramID),
					slog.String("phone", contact.Phone),
					slog.String("error", err.Error()),
				)
			} else {
				recipientResult.Success = true
				result.Successful++
				slog.Info("message sent",
					slog.Int64("telegram_id", contact.TelegramID),
					slog.String("phone", contact.Phone),
				)
			}

			result.Results = append(result.Results, recipientResult)
		}

		return nil
	})

	if err != nil {
		// Check for auth errors
		errStr := err.Error()
		if strings.Contains(errStr, "AUTH_KEY_UNREGISTERED") || strings.Contains(errStr, "SESSION_REVOKED") {
			return nil, fmt.Errorf("session expired - please re-authenticate this account")
		}
		return nil, fmt.Errorf("telegram client error: %w", err)
	}

	return result, nil
}

func sendMessage(ctx context.Context, sender *message.Sender, peer tg.InputPeerClass, text, username string) error {
	_, err := sender.To(peer).Text(ctx, text)
	if err == nil {
		return nil
	}

	// Try to resolve by username if peer is invalid
	if strings.Contains(err.Error(), "PEER_ID_INVALID") && len(username) > 0 {
		resolvedPeer, resolveErr := resolveUsername(ctx, sender, username)
		if resolveErr != nil {
			return fmt.Errorf("peer invalid and failed to resolve username: %w", resolveErr)
		}
		return sendMessage(ctx, sender, resolvedPeer, text, "")
	}

	// Handle flood wait
	if flood, floodErr := tgerr.FloodWait(ctx, err); flood {
		slog.Info("flood wait, retrying...")
		return sendMessage(ctx, sender, peer, text, username)
	} else if floodErr != nil {
		return floodErr
	}

	return err
}

func resolveUsername(ctx context.Context, sender *message.Sender, username string) (tg.InputPeerClass, error) {
	peer, err := sender.Resolve(username).AsInputPeer(ctx)
	if err == nil {
		return peer, nil
	}

	// Handle flood wait
	if flood, floodErr := tgerr.FloodWait(ctx, err); flood {
		return resolveUsername(ctx, sender, username)
	} else if floodErr != nil {
		return nil, floodErr
	}

	return nil, err
}

func formatName(firstName, lastName string) string {
	name := strings.TrimSpace(firstName + " " + lastName)
	if name == "" {
		return "Unknown"
	}
	return name
}
