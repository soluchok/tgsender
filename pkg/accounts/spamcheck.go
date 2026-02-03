package accounts

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"

	tgclient "github.com/soluchok/tgsender/pkg/telegram"
)

const spamCacheTTL = 10 * time.Minute

// SpamStatus represents the spam check result
type SpamStatus struct {
	IsLimited    bool       `json:"is_limited"`
	LimitedUntil *time.Time `json:"limited_until,omitempty"`
	Message      string     `json:"message"`
	CheckedAt    time.Time  `json:"checked_at"`
	FromCache    bool       `json:"from_cache"`
}

// cachedSpamStatus holds cached spam status with expiration
type cachedSpamStatus struct {
	status    *SpamStatus
	expiresAt time.Time
}

// SpamChecker checks if an account is in Telegram's spam filter
type SpamChecker struct {
	store   *Store
	appID   int
	appHash string
	cache   map[string]*cachedSpamStatus
	mu      sync.RWMutex
}

// NewSpamChecker creates a new spam checker
func NewSpamChecker(store *Store, appID int, appHash string) *SpamChecker {
	return &SpamChecker{
		store:   store,
		appID:   appID,
		appHash: appHash,
		cache:   make(map[string]*cachedSpamStatus),
	}
}

// CheckSpamStatus returns cached status or fetches fresh status from @SpamBot
func (s *SpamChecker) CheckSpamStatus(ctx context.Context, accountID string, forceRefresh bool) (*SpamStatus, error) {
	// Check cache first (unless force refresh)
	if !forceRefresh {
		s.mu.RLock()
		if cached, ok := s.cache[accountID]; ok && time.Now().Before(cached.expiresAt) {
			status := *cached.status // Copy
			status.FromCache = true
			s.mu.RUnlock()
			return &status, nil
		}
		s.mu.RUnlock()
	}

	// Fetch fresh status
	status, err := s.fetchSpamStatus(ctx, accountID)
	if err != nil {
		return nil, err
	}

	// Cache the result
	s.mu.Lock()
	s.cache[accountID] = &cachedSpamStatus{
		status:    status,
		expiresAt: time.Now().Add(spamCacheTTL),
	}
	s.mu.Unlock()

	return status, nil
}

// fetchSpamStatus sends /start to @SpamBot and parses the response
func (s *SpamChecker) fetchSpamStatus(ctx context.Context, accountID string) (*SpamStatus, error) {
	// Get account to access proxy settings
	account, ok := s.store.Get(accountID)
	if !ok {
		return nil, fmt.Errorf("account not found")
	}

	sessionPath := ".data/account_" + accountID + ".json"

	client, err := tgclient.CreateClient(s.appID, s.appHash, sessionPath, account.ProxyURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	var status *SpamStatus

	err = client.Run(ctx, func(ctx context.Context) error {
		api := client.API()

		// Resolve @SpamBot username
		resolved, err := api.ContactsResolveUsername(ctx, "SpamBot")
		if err != nil {
			return fmt.Errorf("failed to resolve @SpamBot: %w", err)
		}

		// Find the bot user
		var botUser *tg.User
		for _, u := range resolved.Users {
			if user, ok := u.(*tg.User); ok && user.Bot {
				botUser = user
				break
			}
		}

		if botUser == nil {
			return fmt.Errorf("SpamBot not found")
		}

		// Create sender and send /start command
		sender := message.NewSender(api)
		peer := &tg.InputPeerUser{
			UserID:     botUser.ID,
			AccessHash: botUser.AccessHash,
		}

		_, err = sender.To(peer).Text(ctx, "/start")
		if err != nil {
			return fmt.Errorf("failed to send /start to SpamBot: %w", err)
		}

		// Wait a bit for the response
		time.Sleep(2 * time.Second)

		// Get messages from SpamBot
		messages, err := api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
			Peer:  peer,
			Limit: 5,
		})
		if err != nil {
			return fmt.Errorf("failed to get messages from SpamBot: %w", err)
		}

		// Parse the response
		status = parseSpamBotResponse(messages)
		status.CheckedAt = time.Now()
		return nil
	})

	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "AUTH_KEY_UNREGISTERED") ||
			strings.Contains(errStr, "SESSION_REVOKED") ||
			strings.Contains(errStr, "USER_DEACTIVATED") {
			return nil, fmt.Errorf("session expired - please re-authenticate")
		}
		return nil, err
	}

	return status, nil
}

// ClearCache removes cached status for an account
func (s *SpamChecker) ClearCache(accountID string) {
	s.mu.Lock()
	delete(s.cache, accountID)
	s.mu.Unlock()
}

// parseSpamBotResponse extracts spam status from SpamBot messages
func parseSpamBotResponse(messages tg.MessagesMessagesClass) *SpamStatus {
	status := &SpamStatus{
		IsLimited: false,
		Message:   "",
	}

	var msgs []tg.MessageClass
	switch m := messages.(type) {
	case *tg.MessagesMessages:
		msgs = m.Messages
	case *tg.MessagesMessagesSlice:
		msgs = m.Messages
	case *tg.MessagesChannelMessages:
		msgs = m.Messages
	default:
		return status
	}

	// Look for the most recent message from SpamBot
	for _, msgClass := range msgs {
		msg, ok := msgClass.(*tg.Message)
		if !ok {
			continue
		}

		text := msg.Message
		if text == "" {
			continue
		}

		status.Message = text

		// Check for limitation indicators
		limitedIndicators := []string{
			"account is now limited",
			"your account is limited",
			"account will be automatically released",
			"moderators have confirmed",
			"found your messages annoying",
		}

		for _, indicator := range limitedIndicators {
			if strings.Contains(strings.ToLower(text), strings.ToLower(indicator)) {
				status.IsLimited = true
				break
			}
		}

		// Try to extract the limitation end date
		// Pattern: "until 28 Jan 2026, 15:06 UTC" or similar
		datePattern := regexp.MustCompile(`until\s+(\d{1,2}\s+\w+\s+\d{4},?\s+\d{1,2}:\d{2}\s*(?:UTC)?)`)
		if matches := datePattern.FindStringSubmatch(text); len(matches) > 1 {
			dateStr := matches[1]
			// Try parsing various formats
			formats := []string{
				"2 Jan 2006, 15:04 UTC",
				"2 Jan 2006 15:04 UTC",
				"02 Jan 2006, 15:04 UTC",
				"02 Jan 2006 15:04 UTC",
			}
			for _, format := range formats {
				if t, err := time.Parse(format, dateStr); err == nil {
					status.LimitedUntil = &t
					break
				}
			}
		}

		// Only process the first meaningful message
		if status.Message != "" {
			break
		}
	}

	// Check for "good standing" message
	goodIndicators := []string{
		"your account is free",
		"no limits",
		"good standing",
		"not limited",
	}

	for _, indicator := range goodIndicators {
		if strings.Contains(strings.ToLower(status.Message), strings.ToLower(indicator)) {
			status.IsLimited = false
			status.LimitedUntil = nil
			break
		}
	}

	return status
}
