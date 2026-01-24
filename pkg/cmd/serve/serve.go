package serve

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/soluchok/tgsender/pkg/accounts"
	"github.com/soluchok/tgsender/pkg/auth"
	"github.com/soluchok/tgsender/pkg/contacts"
	"github.com/soluchok/tgsender/pkg/messages"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	flagAppIDName  = "app-id"
	flagAppIDUsage = "Telegram's APP id (required)"

	flagAppHashName  = "app-hash"
	flagAppHashUsage = "Telegram's APP hash (required)"

	flagBotTokenName  = "bot-token"
	flagBotTokenUsage = "Telegram bot token for OAuth authentication (required)"

	flagListenAddrName  = "listen-addr"
	flagListenAddrValue = "0.0.0.0:8888"
	flagListenAddrUsage = "Server address for listening"

	flagStaticDirName  = "static-dir"
	flagStaticDirValue = ""
	flagStaticDirUsage = "Directory to serve static files from (e.g., web/dist)"
)

func New() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "serve",
		Short: "Run a web server",
		PreRun: func(cmd *cobra.Command, args []string) {
			viper.BindPFlag(flagAppIDName, cmd.PersistentFlags().Lookup(flagAppIDName))
			viper.BindPFlag(flagAppHashName, cmd.PersistentFlags().Lookup(flagAppHashName))
			viper.BindPFlag(flagBotTokenName, cmd.PersistentFlags().Lookup(flagBotTokenName))
			viper.BindPFlag(flagListenAddrName, cmd.PersistentFlags().Lookup(flagListenAddrName))
			viper.BindPFlag(flagStaticDirName, cmd.PersistentFlags().Lookup(flagStaticDirName))
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM, os.Kill)
			defer cancel()

			var cfg *config
			if err := errors.Join(viper.Unmarshal(&cfg), cfg.Validate()); err != nil {
				return err
			}

			// Initialize auth handler
			authHandler := auth.NewHandler(
				cfg.BotToken,
				24*time.Hour,  // Session TTL
				5*time.Minute, // Max age for Telegram auth data
			)

			// Initialize accounts store
			accountStore, err := accounts.NewStore(".data")
			if err != nil {
				return err
			}

			// Initialize QR auth manager
			qrManager := accounts.NewQRAuthManager(accountStore, cfg.AppID, cfg.AppHash)

			// Initialize session validator
			accountValidator := accounts.NewValidator(accountStore, cfg.AppID, cfg.AppHash)

			// Initialize accounts handler
			accountsHandler := accounts.NewHandler(accountStore, qrManager, accountValidator, authHandler)

			// Initialize contacts store and handler
			contactStore, err := contacts.NewStore(".data")
			if err != nil {
				return err
			}
			contactChecker := contacts.NewChecker(contactStore, cfg.AppID, cfg.AppHash)
			jobManager := contacts.NewJobManager(contactChecker)
			contactsHandler := contacts.NewHandler(contactStore, contactChecker, accountStore, authHandler, jobManager)

			var mux = http.NewServeMux()

			// Auth routes
			mux.HandleFunc("/api/auth/telegram", authHandler.HandleTelegramAuth)
			mux.HandleFunc("/api/auth/me", authHandler.HandleMe)
			mux.HandleFunc("/api/auth/logout", authHandler.HandleLogout)

			// Accounts routes
			mux.HandleFunc("/api/accounts", accountsHandler.HandleListAccounts)
			mux.HandleFunc("/api/accounts/{id}", accountsHandler.HandleDeleteAccount)
			mux.HandleFunc("/api/accounts/{id}/validate", accountsHandler.HandleValidateAccount)
			mux.HandleFunc("/api/accounts/{id}/settings", func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					accountsHandler.HandleGetSettings(w, r)
				} else if r.Method == http.MethodPut {
					accountsHandler.HandleUpdateSettings(w, r)
				} else {
					http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				}
			})
			mux.HandleFunc("/api/accounts/qr/start", accountsHandler.HandleStartQRAuth)
			mux.HandleFunc("/api/accounts/qr/status", accountsHandler.HandleQRAuthStatus)
			mux.HandleFunc("/api/accounts/qr/cancel", accountsHandler.HandleCancelQRAuth)
			mux.HandleFunc("/api/accounts/qr/password", accountsHandler.HandleSubmitPassword)

			// Contacts routes
			mux.HandleFunc("/api/accounts/{id}/check-numbers", contactsHandler.HandleCheckNumbers)
			mux.HandleFunc("/api/accounts/{id}/contacts", contactsHandler.HandleListContacts)
			mux.HandleFunc("/api/accounts/{id}/import-chats", contactsHandler.HandleImportFromChats)
			mux.HandleFunc("/api/accounts/{id}/import-chats/status", contactsHandler.HandleImportFromChatsStatus)
			mux.HandleFunc("/api/accounts/{id}/import-contacts", contactsHandler.HandleImportContacts)
			mux.HandleFunc("/api/contacts/{id}", contactsHandler.HandleDeleteContact)
			mux.HandleFunc("/api/contacts/{id}/update", contactsHandler.HandleUpdateContact)

			// Messages routes
			messageSender := messages.NewSender(contactStore, cfg.AppID, cfg.AppHash)
			jobStore, err := messages.NewJobStore(".data")
			if err != nil {
				return err
			}
			messagesHandler := messages.NewHandler(messageSender, jobStore, accountStore, authHandler)
			mux.HandleFunc("/api/accounts/{id}/send", messagesHandler.HandleSendMessages)
			mux.HandleFunc("/api/accounts/{id}/send/status", messagesHandler.HandleSendStatus)
			mux.HandleFunc("/api/accounts/{id}/send/retry", messagesHandler.HandleRetryFailed)
			mux.HandleFunc("/api/accounts/{id}/send/history", messagesHandler.HandleSendHistory)

			// Health check
			mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			})

			// Serve static files if configured
			if cfg.StaticDir != "" {
				slog.Info("serving static files", "dir", cfg.StaticDir)
				mux.Handle("/", spaHandler(cfg.StaticDir))
			}

			var server = &http.Server{
				Addr:    cfg.ListenAddr,
				Handler: corsMiddleware(mux),
			}
			context.AfterFunc(ctx, func() { server.Close() })

			slog.Info("server starting", "addr", server.Addr)
			if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				return err
			}

			slog.Info("server was closed")

			return nil
		},
	}

	cmd.PersistentFlags().Int(flagAppIDName, 0, flagAppIDUsage)
	cmd.PersistentFlags().String(flagAppHashName, "", flagAppHashUsage)
	cmd.PersistentFlags().String(flagBotTokenName, "", flagBotTokenUsage)
	cmd.PersistentFlags().String(flagListenAddrName, flagListenAddrValue, flagListenAddrUsage)
	cmd.PersistentFlags().String(flagStaticDirName, flagStaticDirValue, flagStaticDirUsage)

	return cmd
}

// spaHandler returns a handler that serves static files and falls back to index.html for SPA routing
func spaHandler(staticDir string) http.Handler {
	fileServer := http.FileServer(http.Dir(staticDir))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Check if file exists
		fullPath := staticDir + path
		if _, err := os.Stat(fullPath); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		// Check if it's a file request (has extension) that doesn't exist
		if strings.Contains(path, ".") {
			http.NotFound(w, r)
			return
		}

		// For SPA routes, serve index.html
		http.ServeFile(w, r, staticDir+"/index.html")
	})
}

// corsMiddleware adds CORS headers for development
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
