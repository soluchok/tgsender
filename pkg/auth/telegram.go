package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// TelegramUser represents a user authenticated via Telegram Login Widget
type TelegramUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
	PhotoURL  string `json:"photo_url,omitempty"`
	AuthDate  int64  `json:"auth_date"`
	Hash      string `json:"hash"`
}

// Validate verifies the Telegram authentication data
// botToken is the token received from @BotFather
// maxAge is the maximum allowed age of the auth_date (0 for no limit)
func (u *TelegramUser) Validate(botToken string, maxAge time.Duration) error {
	// Check auth_date is not too old
	if maxAge > 0 {
		authTime := time.Unix(u.AuthDate, 0)
		if time.Since(authTime) > maxAge {
			return fmt.Errorf("authentication data is expired")
		}
	}

	// Build the data-check-string
	checkString := u.buildCheckString()

	// Create secret key: SHA256(bot_token)
	secretKey := sha256.Sum256([]byte(botToken))

	// Calculate HMAC-SHA256
	h := hmac.New(sha256.New, secretKey[:])
	h.Write([]byte(checkString))
	calculatedHash := hex.EncodeToString(h.Sum(nil))

	// Compare hashes
	if !hmac.Equal([]byte(calculatedHash), []byte(u.Hash)) {
		return fmt.Errorf("invalid authentication hash")
	}

	return nil
}

// buildCheckString creates the data-check-string for hash verification
func (u *TelegramUser) buildCheckString() string {
	data := make(map[string]string)

	data["id"] = strconv.FormatInt(u.ID, 10)
	data["first_name"] = u.FirstName
	data["auth_date"] = strconv.FormatInt(u.AuthDate, 10)

	if u.LastName != "" {
		data["last_name"] = u.LastName
	}
	if u.Username != "" {
		data["username"] = u.Username
	}
	if u.PhotoURL != "" {
		data["photo_url"] = u.PhotoURL
	}

	// Sort keys alphabetically
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build the check string
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, data[k]))
	}

	return strings.Join(parts, "\n")
}

// ParseFromQuery parses Telegram user data from URL query parameters
func ParseFromQuery(values url.Values) (*TelegramUser, error) {
	idStr := values.Get("id")
	if idStr == "" {
		return nil, fmt.Errorf("missing id parameter")
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid id parameter: %w", err)
	}

	authDateStr := values.Get("auth_date")
	if authDateStr == "" {
		return nil, fmt.Errorf("missing auth_date parameter")
	}

	authDate, err := strconv.ParseInt(authDateStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid auth_date parameter: %w", err)
	}

	hash := values.Get("hash")
	if hash == "" {
		return nil, fmt.Errorf("missing hash parameter")
	}

	firstName := values.Get("first_name")
	if firstName == "" {
		return nil, fmt.Errorf("missing first_name parameter")
	}

	return &TelegramUser{
		ID:        id,
		FirstName: firstName,
		LastName:  values.Get("last_name"),
		Username:  values.Get("username"),
		PhotoURL:  values.Get("photo_url"),
		AuthDate:  authDate,
		Hash:      hash,
	}, nil
}
