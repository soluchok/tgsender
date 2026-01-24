package accounts

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"rsc.io/qr"
)

// QRAuthState represents the state of a QR authentication session
type QRAuthState struct {
	Token     string    `json:"token"`
	Status    string    `json:"status"` // pending, scanning, password_required, success, error, expired
	QRURL     string    `json:"qr_url,omitempty"`
	Error     string    `json:"error,omitempty"`
	Account   *Account  `json:"account,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
}

// memorySession is an in-memory session storage that can be saved to file later
type memorySession struct {
	mu   sync.Mutex
	data []byte
}

func (m *memorySession) LoadSession(_ context.Context) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data == nil {
		return nil, nil
	}
	return append([]byte(nil), m.data...), nil
}

func (m *memorySession) StoreSession(_ context.Context, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = append([]byte(nil), data...)
	return nil
}

func (m *memorySession) SaveToFile(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data == nil {
		return fmt.Errorf("no session data to save")
	}
	return os.WriteFile(path, m.data, 0600)
}

// QRAuthManager manages QR code authentication sessions
type QRAuthManager struct {
	mu       sync.RWMutex
	sessions map[string]*qrSession
	store    *Store
	appID    int
	appHash  string
}

type qrSession struct {
	state         *QRAuthState
	ownerID       int64
	cancel        context.CancelFunc
	client        *telegram.Client
	memorySession *memorySession // In-memory session storage
	passwordCh    chan string    // Channel to receive 2FA password
}

// NewQRAuthManager creates a new QR auth manager
func NewQRAuthManager(store *Store, appID int, appHash string) *QRAuthManager {
	return &QRAuthManager{
		sessions: make(map[string]*qrSession),
		store:    store,
		appID:    appID,
		appHash:  appHash,
	}
}

// StartAuth initiates a new QR authentication session
func (m *QRAuthManager) StartAuth(ownerID int64) (*QRAuthState, error) {
	token, err := generateID()
	if err != nil {
		return nil, err
	}

	state := &QRAuthState{
		Token:     token,
		Status:    "pending",
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)

	session := &qrSession{
		state:      state,
		ownerID:    ownerID,
		cancel:     cancel,
		passwordCh: make(chan string, 1),
	}

	m.mu.Lock()
	m.sessions[token] = session
	m.mu.Unlock()

	// Start QR auth in background
	go m.runQRAuth(ctx, session)

	// Wait a bit for QR to be generated
	time.Sleep(3 * time.Second)

	m.mu.RLock()
	currentState := *session.state
	m.mu.RUnlock()

	return &currentState, nil
}

// GetStatus returns the current status of a QR auth session
func (m *QRAuthManager) GetStatus(token string) (*QRAuthState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, ok := m.sessions[token]
	if !ok {
		return nil, false
	}

	// Check if expired (but not for password_required or success status)
	if time.Now().After(session.state.ExpiresAt) &&
		session.state.Status != "success" &&
		session.state.Status != "password_required" {
		session.state.Status = "expired"
		session.cancel()
	}

	stateCopy := *session.state
	return &stateCopy, true
}

// SubmitPassword submits the 2FA password for a session
func (m *QRAuthManager) SubmitPassword(token, password string) error {
	m.mu.RLock()
	session, ok := m.sessions[token]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("session not found")
	}

	//scanning
	fmt.Println("session.state.Status", session.state.Status)
	if session.state.Status != "password_required" {
		return fmt.Errorf("password not required")
	}

	select {
	case session.passwordCh <- password:
		return nil
	default:
		return fmt.Errorf("password channel full")
	}
}

// CancelAuth cancels an ongoing QR auth session
func (m *QRAuthManager) CancelAuth(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, ok := m.sessions[token]; ok {
		session.cancel()
		delete(m.sessions, token)
	}
}

func (m *QRAuthManager) runQRAuth(ctx context.Context, session *qrSession) {
	// Don't delete session immediately on completion - keep it for a while so frontend can poll status
	defer func() {
		// Keep session for 30 seconds after completion so frontend can fetch final status
		go func() {
			time.Sleep(30 * time.Second)
			m.mu.Lock()
			delete(m.sessions, session.state.Token)
			m.mu.Unlock()
		}()
	}()

	// Ensure .data directory exists
	if err := os.MkdirAll(".data", 0700); err != nil {
		slog.Error("failed to create session directory", "error", err)
		m.mu.Lock()
		session.state.Status = "error"
		session.state.Error = "Failed to create session directory"
		m.mu.Unlock()
		return
	}

	// Use in-memory session storage during auth - will save to file only on success
	session.memorySession = &memorySession{}

	// Create update dispatcher for handling login token updates
	dispatcher := tg.NewUpdateDispatcher()

	// Channel to signal when login token is received
	loginTokenCh := make(chan struct{}, 1)

	dispatcher.OnLoginToken(func(ctx context.Context, e tg.Entities, update *tg.UpdateLoginToken) error {
		slog.Info("received login token update")
		select {
		case loginTokenCh <- struct{}{}:
		default:
		}
		return nil
	})

	client := telegram.NewClient(m.appID, m.appHash, telegram.Options{
		SessionStorage: session.memorySession,
		UpdateHandler:  dispatcher,
	})
	session.client = client

	err := client.Run(ctx, func(ctx context.Context) error {
		for {
			slog.Info("exporting login token")

			// Export login token (generates QR code data)
			resp, err := client.API().AuthExportLoginToken(ctx, &tg.AuthExportLoginTokenRequest{
				APIID:     m.appID,
				APIHash:   m.appHash,
				ExceptIDs: []int64{},
			})
			if err != nil {
				return fmt.Errorf("export login token: %w", err)
			}

			slog.Info("got response", "type", fmt.Sprintf("%T", resp))

			switch v := resp.(type) {
			case *tg.AuthLoginToken:
				// Generate QR code URL
				qrURL := fmt.Sprintf("tg://login?token=%s", base64.URLEncoding.EncodeToString(v.Token))
				slog.Info("generated QR URL", "url", qrURL)

				// Generate QR code image
				qrPNG, err := generateQRCode(qrURL)
				if err != nil {
					return fmt.Errorf("generate QR code: %w", err)
				}

				m.mu.Lock()
				session.state.Status = "scanning"
				session.state.QRURL = qrPNG
				m.mu.Unlock()

				// Calculate wait duration
				waitDuration := time.Duration(v.Expires-int(time.Now().Unix())) * time.Second
				if waitDuration < 0 {
					waitDuration = 30 * time.Second
				}

				slog.Info("waiting for scan", "duration", waitDuration)

				// Wait for token to be scanned or timeout
				select {
				case <-loginTokenCh:
					slog.Info("login token received, importing...")
					// Token was scanned, need to import it
					if err := m.handleTokenImport(ctx, client, session, v.Token); err != nil {
						return err
					}
					// If we got here without error, check if we need to continue
					if session.state.Status == "success" {
						return nil
					}
					// Otherwise continue loop (might need new token)

				case <-time.After(waitDuration):
					slog.Info("token expired, generating new one")
					// Token expired, loop to get new one
					continue
				case <-ctx.Done():
					return ctx.Err()
				}

			case *tg.AuthLoginTokenMigrateTo:
				slog.Info("DC migration required")
				return fmt.Errorf("DC migration required (not implemented)")

			case *tg.AuthLoginTokenSuccess:
				slog.Info("login success!")
				// Only handle if not already successful (prevent duplicate)
				m.mu.RLock()
				alreadySuccess := session.state.Status == "success"
				m.mu.RUnlock()
				if alreadySuccess {
					return nil
				}
				return m.handleLoginSuccess(ctx, client, session, v)
			}
		}
	})

	// After client.Run() completes, save session to file if login was successful
	if session.state.Status == "success" && session.state.Account != nil {
		sessionPath := fmt.Sprintf(".data/account_%s.json", session.state.Account.ID)
		if err := session.memorySession.SaveToFile(sessionPath); err != nil {
			slog.Error("failed to save session file", "error", err)
		} else {
			slog.Info("session saved to file", "path", sessionPath)
		}
	}

	if err != nil && session.state.Status != "success" {
		slog.Error("QR auth error", "error", err)
		m.mu.Lock()
		if session.state.Status != "expired" && session.state.Status != "password_required" {
			session.state.Status = "error"
			if session.state.Error == "" {
				session.state.Error = err.Error()
			}
		}
		m.mu.Unlock()
	}
}

func (m *QRAuthManager) handleTokenImport(ctx context.Context, client *telegram.Client, session *qrSession, token []byte) error {
	importResp, err := client.API().AuthImportLoginToken(ctx, token)
	if err != nil {
		// Check if 2FA is required
		if tgerr.Is(err, "SESSION_PASSWORD_NEEDED") {
			slog.Info("2FA password required")
			return m.handle2FA(ctx, client, session)
		}
		slog.Error("import token error", "error", err)
		return fmt.Errorf("import login token: %w", err)
	}

	slog.Info("import response", "type", fmt.Sprintf("%T", importResp))

	// Check the response type
	switch iv := importResp.(type) {
	case *tg.AuthLoginTokenSuccess:
		// Only handle if not already successful (prevent duplicate)
		m.mu.RLock()
		alreadySuccess := session.state.Status == "success"
		m.mu.RUnlock()
		if alreadySuccess {
			return nil
		}
		return m.handleLoginSuccess(ctx, client, session, iv)
	case *tg.AuthLoginTokenMigrateTo:
		return fmt.Errorf("DC migration required (not implemented)")
	case *tg.AuthLoginToken:
		// Need to continue - loop will handle it
		slog.Info("got another token, continuing loop")
		return nil
	}

	return nil
}

func (m *QRAuthManager) handle2FA(ctx context.Context, client *telegram.Client, session *qrSession) error {
	// Get password settings
	pwd, err := client.API().AccountGetPassword(ctx)
	if err != nil {
		return fmt.Errorf("get password settings: %w", err)
	}

	// Update state to request password
	m.mu.Lock()
	session.state.Status = "password_required"
	session.state.ExpiresAt = time.Now().Add(5 * time.Minute) // Extend expiry
	m.mu.Unlock()

	slog.Info("waiting for password input")

	// Wait for password from user
	select {
	case password := <-session.passwordCh:
		slog.Info("password received, checking...")

		// Compute SRP password using gotd's auth helper
		srpAnswer, err := auth.PasswordHash(
			[]byte(password),
			pwd.SRPID,
			pwd.SRPB,
			pwd.SecureRandom,
			pwd.CurrentAlgo,
		)
		if err != nil {
			m.mu.Lock()
			session.state.Status = "error"
			session.state.Error = "Failed to process password"
			m.mu.Unlock()
			return fmt.Errorf("compute SRP: %w", err)
		}

		// Check password
		authResult, err := client.API().AuthCheckPassword(ctx, srpAnswer)
		if err != nil {
			if tgerr.Is(err, "PASSWORD_HASH_INVALID") {
				m.mu.Lock()
				session.state.Status = "error"
				session.state.Error = "Invalid password"
				m.mu.Unlock()
				return fmt.Errorf("invalid password")
			}
			return fmt.Errorf("check password: %w", err)
		}

		// Get user from auth result
		authAuth, ok := authResult.(*tg.AuthAuthorization)
		if !ok {
			return fmt.Errorf("unexpected auth result type: %T", authResult)
		}

		user, ok := authAuth.User.AsNotEmpty()
		if !ok {
			return fmt.Errorf("empty user after 2FA")
		}

		slog.Info("2FA successful", "user_id", user.ID)

		// Download profile photo
		photoURL := downloadProfilePhoto(ctx, client, user)

		// Create account with TelegramID as the account ID
		// Note: Session file will be renamed after client.Run() completes
		account := &Account{
			ID:         fmt.Sprintf("%d", user.ID),
			OwnerID:    session.ownerID,
			TelegramID: user.ID,
			Phone:      user.Phone,
			FirstName:  user.FirstName,
			LastName:   user.LastName,
			Username:   user.Username,
			PhotoURL:   photoURL,
			IsActive:   true,
		}

		if err := m.store.Create(account); err != nil {
			return fmt.Errorf("save account: %w", err)
		}

		m.mu.Lock()
		session.state.Status = "success"
		session.state.Account = account
		m.mu.Unlock()

		return nil

	case <-time.After(5 * time.Minute):
		m.mu.Lock()
		session.state.Status = "expired"
		session.state.Error = "Password input timed out"
		m.mu.Unlock()
		return fmt.Errorf("password timeout")

	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *QRAuthManager) handleLoginSuccess(ctx context.Context, client *telegram.Client, session *qrSession, v *tg.AuthLoginTokenSuccess) error {
	authAuth, ok := v.Authorization.(*tg.AuthAuthorization)
	if !ok {
		return fmt.Errorf("unexpected authorization type: %T", v.Authorization)
	}

	user, ok := authAuth.User.AsNotEmpty()
	if !ok {
		return fmt.Errorf("empty user")
	}

	slog.Info("login successful", "user_id", user.ID, "username", user.Username)

	// Download profile photo
	photoURL := downloadProfilePhoto(ctx, client, user)

	// Create account with TelegramID as the account ID
	// Note: Session file will be renamed after client.Run() completes
	account := &Account{
		ID:         fmt.Sprintf("%d", user.ID),
		OwnerID:    session.ownerID,
		TelegramID: user.ID,
		Phone:      user.Phone,
		FirstName:  user.FirstName,
		LastName:   user.LastName,
		Username:   user.Username,
		PhotoURL:   photoURL,
		IsActive:   true,
	}

	// Save account
	if err := m.store.Create(account); err != nil {
		return fmt.Errorf("save account: %w", err)
	}

	m.mu.Lock()
	session.state.Status = "success"
	session.state.Account = account
	m.mu.Unlock()

	slog.Info("account created successfully", "account_id", account.ID)

	return nil
}

func generateQRCode(url string) (string, error) {
	code, err := qr.Encode(url, qr.M)
	if err != nil {
		return "", err
	}

	png := code.PNG()
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}

// downloadProfilePhoto downloads the profile photo for a user
func downloadProfilePhoto(ctx context.Context, client *telegram.Client, user *tg.User) string {
	photo, ok := user.Photo.AsNotEmpty()
	if !ok {
		return "" // No photo set
	}

	d := downloader.NewDownloader()
	var buf strings.Builder
	writer := base64.NewEncoder(base64.StdEncoding, &buf)

	_, err := d.Download(client.API(), &tg.InputPeerPhotoFileLocation{
		Peer:    user.AsInputPeer(),
		PhotoID: photo.PhotoID,
	}).Stream(ctx, writer)
	writer.Close()

	if err != nil {
		slog.Warn("failed to download profile photo", "error", err)
		return ""
	}

	return "data:image/jpeg;base64," + buf.String()
}
