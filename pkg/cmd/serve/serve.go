package serve

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
	"github.com/soluchok/tgsender/pkg/session"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	flagAppIDName  = "app-id"
	flagAppIDUsage = "Telegram's APP id (required)"

	flagAppHashName  = "app-hash"
	flagAppHashUsage = "Telegram's APP hash (required)"
)

func New() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "serve",
		Short: "Run a web server",
		PreRun: func(cmd *cobra.Command, args []string) {
			viper.BindPFlag(flagAppIDName, cmd.PersistentFlags().Lookup(flagAppIDName))
			viper.BindPFlag(flagAppHashName, cmd.PersistentFlags().Lookup(flagAppHashName))
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM, os.Kill)
			defer cancel()

			var cfg *config
			if err := errors.Join(viper.Unmarshal(&cfg), cfg.Validate()); err != nil {
				return err
			}

			var mux = http.NewServeMux()
			mux.Handle("/session/all", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sessions, err := session.All()
				if err != nil {
					json.NewEncoder(w).Encode(map[string]string{
						"error": err.Error(),
					})
					return
				}

				var result []string
				for _, store := range sessions {
					var client = telegram.NewClient(cfg.AppID, cfg.AppHash, telegram.Options{SessionStorage: store})
					err := client.Run(cmd.Context(), func(ctx context.Context) error {
						self, err := client.Self(r.Context())
						if err != nil {
							return err
						}
						p, _ := self.Photo.AsNotEmpty()

						var down = downloader.NewDownloader()
						bb := down.Download(client.API(), &tg.InputPeerPhotoFileLocation{
							Peer:    self.AsInputPeer(),
							PhotoID: p.PhotoID,
						})

						// var out bytes.Buffer

						bb.ToPath(ctx, "ttt")
						// aa, _ := bb.Stream(ctx, &out)
						// fmt.Println(out.String())
						// fmt.Println()
						// fmt.Println(aa.TypeName())
						result = append(result, self.FirstName, self.LastName)

						return nil
					})

					if err != nil {
						json.NewEncoder(w).Encode(map[string]string{
							"error": err.Error(),
						})
						return
					}
				}

				json.NewEncoder(w).Encode(result)

			}))

			var server = &http.Server{Addr: ":8888", Handler: mux}
			context.AfterFunc(ctx, func() { server.Close() })
			if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				return err
			}

			slog.Info("server was closed")

			return nil
		},
	}

	cmd.PersistentFlags().Int(flagAppIDName, 0, flagAppIDUsage)
	cmd.PersistentFlags().String(flagAppHashName, "", flagAppHashUsage)

	return cmd
}
