package messages

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"github.com/soluchok/tgsender/pkg/contacts"
	"github.com/soluchok/tgsender/pkg/openai"
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
func (s *Sender) SendToContacts(ctx context.Context, sessionPath string, contactIDs []string, messageText string, delayMinMS, delayMaxMS int) (*SendResult, error) {
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

		for i, contact := range contactsToSend {
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

			// Process message template for this contact
			processedMessage, err := processMessageTemplate(messageText, contact)
			if err != nil {
				recipientResult.Success = false
				recipientResult.Error = fmt.Sprintf("template error: %v", err)
				result.Failed++
				slog.Error("failed to process message template",
					slog.Int64("telegram_id", contact.TelegramID),
					slog.String("phone", contact.Phone),
					slog.String("error", err.Error()),
				)
				result.Results = append(result.Results, recipientResult)
				continue
			}

			// Create peer
			peer := &tg.InputPeerUser{
				UserID:     contact.TelegramID,
				AccessHash: contact.AccessHash,
			}

			// Send message
			err = sendMessage(ctx, sender, peer, processedMessage, contact.Username)
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

			// Add random delay between messages (except after the last one)
			if delayMaxMS > 0 && i < len(contactsToSend)-1 {
				// Calculate random delay between min and max
				delayMS := delayMinMS
				if delayMaxMS > delayMinMS {
					delayMS = delayMinMS + rand.Intn(delayMaxMS-delayMinMS+1)
				}
				delay := time.Duration(delayMS) * time.Millisecond

				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
				}
			}
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

// SendToContactsWithProgress sends a message to the specified contacts with progress callback
func (s *Sender) SendToContactsWithProgress(ctx context.Context, sessionPath string, contactIDs []string, messageText string, delayMinMS, delayMaxMS int, aiPrompt, openAIToken string, onProgress func(sent, failed int, results []RecipientResult)) (*SendResult, error) {
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

	// Create OpenAI client if AI rewriting is enabled
	var openAIClient *openai.Client
	if aiPrompt != "" && openAIToken != "" {
		openAIClient = openai.NewClient(openAIToken)
		slog.Info("AI message rewriting enabled")
	}

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

		for i, contact := range contactsToSend {
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
				if onProgress != nil {
					onProgress(result.Successful, result.Failed, result.Results)
				}
				continue
			}

			sent[contact.TelegramID] = true

			// Process message template for this contact first
			processedMessage, err := processMessageTemplate(messageText, contact)
			if err != nil {
				recipientResult.Success = false
				recipientResult.Error = fmt.Sprintf("template error: %v", err)
				result.Failed++
				slog.Error("failed to process message template",
					slog.Int64("telegram_id", contact.TelegramID),
					slog.String("phone", contact.Phone),
					slog.String("error", err.Error()),
				)
				result.Results = append(result.Results, recipientResult)
				if onProgress != nil {
					onProgress(result.Successful, result.Failed, result.Results)
				}
				continue
			}

			// Use AI to rewrite the personalized message if enabled
			if openAIClient != nil {
				rewrittenMessage, err := openAIClient.RewriteMessage(ctx, processedMessage, aiPrompt)
				if err != nil {
					slog.Warn("AI rewrite failed, using original message",
						slog.String("phone", contact.Phone),
						slog.String("error", err.Error()),
					)
					// Continue with processed message if AI fails
				} else {
					processedMessage = rewrittenMessage
					slog.Debug("message rewritten by AI",
						slog.String("phone", contact.Phone),
					)
				}
			}

			// Create peer
			peer := &tg.InputPeerUser{
				UserID:     contact.TelegramID,
				AccessHash: contact.AccessHash,
			}

			// Send message
			err = sendMessage(ctx, sender, peer, processedMessage, contact.Username)
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

			// Report progress
			if onProgress != nil {
				onProgress(result.Successful, result.Failed, result.Results)
			}

			// Add random delay between messages (except after the last one)
			if delayMaxMS > 0 && i < len(contactsToSend)-1 {
				// Calculate random delay between min and max
				delayMS := delayMinMS
				if delayMaxMS > delayMinMS {
					delayMS = delayMinMS + rand.Intn(delayMaxMS-delayMinMS+1)
				}
				delay := time.Duration(delayMS) * time.Millisecond

				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
				}
			}
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

// TemplateData contains data available to message templates
type TemplateData struct {
	FirstName string
	LastName  string
	Name      string
	Phone     string
	Username  string
}

// templateFuncs returns the custom template functions
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"pick": func(options ...string) string {
			if len(options) == 0 {
				return ""
			}
			return options[rand.Intn(len(options))]
		},
	}
}

// processMessageTemplate processes the message template with contact data
func processMessageTemplate(messageTemplate string, contact *contacts.Contact) (string, error) {
	tmpl, err := template.New("message").Funcs(templateFuncs()).Parse(messageTemplate)
	if err != nil {
		return "", fmt.Errorf("invalid template: %w", err)
	}

	data := TemplateData{
		FirstName: contact.FirstName,
		LastName:  contact.LastName,
		Name:      formatName(contact.FirstName, contact.LastName),
		Phone:     contact.Phone,
		Username:  contact.Username,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("template execution failed: %w", err)
	}

	return buf.String(), nil
}
