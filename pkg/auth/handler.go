package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"
)

// Session represents an authenticated user session
type Session struct {
	User      *TelegramUser `json:"user"`
	Token     string        `json:"token"`
	CreatedAt time.Time     `json:"created_at"`
	ExpiresAt time.Time     `json:"expires_at"`
}

// SessionStore manages user sessions (in-memory for simplicity)
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	ttl      time.Duration
}

// NewSessionStore creates a new session store
func NewSessionStore(ttl time.Duration) *SessionStore {
	store := &SessionStore{
		sessions: make(map[string]*Session),
		ttl:      ttl,
	}

	// Start cleanup goroutine
	go store.cleanup()

	return store
}

// Create creates a new session for the user
func (s *SessionStore) Create(user *TelegramUser) (*Session, error) {
	token, err := generateToken(32)
	if err != nil {
		return nil, err
	}

	session := &Session{
		User:      user,
		Token:     token,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(s.ttl),
	}

	s.mu.Lock()
	s.sessions[token] = session
	s.mu.Unlock()

	return session, nil
}

// Get retrieves a session by token
func (s *SessionStore) Get(token string) (*Session, bool) {
	s.mu.RLock()
	session, ok := s.sessions[token]
	s.mu.RUnlock()

	if !ok {
		return nil, false
	}

	if time.Now().After(session.ExpiresAt) {
		s.Delete(token)
		return nil, false
	}

	return session, true
}

// Delete removes a session
func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

// cleanup periodically removes expired sessions
func (s *SessionStore) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for token, session := range s.sessions {
			if now.After(session.ExpiresAt) {
				delete(s.sessions, token)
			}
		}
		s.mu.Unlock()
	}
}

func generateToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

// Handler provides HTTP handlers for authentication
type Handler struct {
	store    *SessionStore
	botToken string
	maxAge   time.Duration
}

// NewHandler creates a new auth handler
func NewHandler(botToken string, sessionTTL, authMaxAge time.Duration) *Handler {
	return &Handler{
		store:    NewSessionStore(sessionTTL),
		botToken: botToken,
		maxAge:   authMaxAge,
	}
}

// HandleTelegramAuth handles POST /api/auth/telegram
func (h *Handler) HandleTelegramAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var user TelegramUser
	if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate Telegram authentication
	if err := user.Validate(h.botToken, h.maxAge); err != nil {
		writeJSONError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Create session
	session, err := h.store.Create(&user)
	if err != nil {
		writeJSONError(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	// Set cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    session.Token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  session.ExpiresAt,
	})

	writeJSON(w, user, http.StatusOK)
}

// HandleMe handles GET /api/auth/me
func (h *Handler) HandleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	session, ok := h.getSession(r)
	if !ok {
		writeJSONError(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	writeJSON(w, session.User, http.StatusOK)
}

// HandleLogout handles POST /api/auth/logout
func (h *Handler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if cookie, err := r.Cookie("session_token"); err == nil {
		h.store.Delete(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	writeJSON(w, map[string]string{"message": "Logged out"}, http.StatusOK)
}

// AuthMiddleware protects routes requiring authentication
func (h *Handler) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := h.getSession(r); !ok {
			writeJSONError(w, "Not authenticated", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) getSession(r *http.Request) (*Session, bool) {
	cookie, err := r.Cookie("session_token")
	if err != nil {
		return nil, false
	}
	return h.store.Get(cookie.Value)
}

// GetSession returns a session by token (exported for use by other packages)
func (h *Handler) GetSession(token string) (*Session, bool) {
	return h.store.Get(token)
}

// Helper functions for JSON responses
func writeJSON(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeJSONError(w http.ResponseWriter, message string, status int) {
	writeJSON(w, map[string]string{"error": message}, status)
}

// ErrNoBotToken is returned when bot token is not configured
var ErrNoBotToken = errors.New("telegram bot token is required for authentication")
