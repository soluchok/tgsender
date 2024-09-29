package send

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"

	"github.com/gotd/td/examples"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/soluchok/tgsender/pkg/model"
	"github.com/soluchok/tgsender/pkg/session"
)

const (
	flagAppIDName  = "app-id"
	flagAppIDUsage = "Telegram's APP id (required)"

	flagAppHashName  = "app-hash"
	flagAppHashUsage = "Telegram's APP hash (required)"

	flagAuthName      = "auth"
	flagAuthShorthand = "a"
	flagAuthUsage     = "Telegram's phone number for authentication (required)"

	flagInputName  = "input"
	flagInputValue = "users.out"
	flagInputUsage = "input's data file name (required)"

	flagMessageName      = "message"
	flagMessageShorthand = "m"
	flagMessageValue     = ""
	flagMessageUsage     = "Text that will be sent to the intended users (required)"
)

func New() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "send",
		Short: "Send a message to the Telegram users.",
		PreRun: func(cmd *cobra.Command, args []string) {
			viper.BindPFlag(flagAuthName, cmd.PersistentFlags().Lookup(flagAuthName))
			viper.BindPFlag(flagAppIDName, cmd.PersistentFlags().Lookup(flagAppIDName))
			viper.BindPFlag(flagAppHashName, cmd.PersistentFlags().Lookup(flagAppHashName))
			viper.BindPFlag(flagInputName, cmd.PersistentFlags().Lookup(flagInputName))
			viper.BindPFlag(flagMessageName, cmd.PersistentFlags().Lookup(flagMessageName))
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			var total atomic.Int64
			var successful atomic.Int64

			var cfg *config
			if err := errors.Join(viper.Unmarshal(&cfg), cfg.Validate()); err != nil {
				return err
			}

			in, err := os.OpenFile(cfg.Input, os.O_RDWR, 0666)
			if err != nil {
				return err
			}

			defer in.Close()

			store, err := session.Get(cfg.Authentication)
			if err != nil {
				return fmt.Errorf("failed to get session: %w", err)
			}

			var client = telegram.NewClient(cfg.AppID, cfg.AppHash, telegram.Options{SessionStorage: store})
			var flow = auth.NewFlow(examples.Terminal{PhoneNumber: cfg.Authentication}, auth.SendCodeOptions{})
			var sender = message.NewSender(client.API())

			var delivered = map[tg.InputPeerUser]struct{}{}

			defer func() {
				fmt.Println("Total:", total.Load())
				fmt.Println("Successful:", successful.Load())
				fmt.Println("Error:", total.Load()-successful.Load())
			}()

			return client.Run(cmd.Context(), func(ctx context.Context) error {
				if err := client.Auth().IfNecessary(ctx, flow); err != nil {
					return err
				}

				var scanner = bufio.NewScanner(in)
				for scanner.Scan() {
					var user *model.User
					if err := json.Unmarshal(scanner.Bytes(), &user); err != nil {
						return fmt.Errorf("failed to unmarshal user: %w", err)
					}

					var peer = tg.InputPeerUser{
						UserID:     user.ID,
						AccessHash: user.AccessHash,
					}

					if _, ok := delivered[peer]; ok {
						slog.Warn("message was already delivered", slog.String("last_name", user.LastName))
						continue
					}

					delivered[peer] = struct{}{}

					total.Add(1)
					successful.Add(1)
					if err := send(ctx, sender, &peer, cfg.Message, user.Username); err != nil {
						successful.Add(-1)
						slog.Error("failed to send message", slog.Int64("user_id", user.ID), slog.String("username", user.Username), slog.String("error", err.Error()))
					}
				}

				return scanner.Err()
			})
		},
	}

	cmd.PersistentFlags().StringP(flagAuthName, flagAuthShorthand, "", flagAuthUsage)
	cmd.PersistentFlags().Int(flagAppIDName, 0, flagAppIDUsage)
	cmd.PersistentFlags().String(flagAppHashName, "", flagAppHashUsage)
	cmd.PersistentFlags().String(flagInputName, flagInputValue, flagInputUsage)
	cmd.PersistentFlags().StringP(flagMessageName, flagMessageShorthand, flagMessageValue, flagMessageUsage)

	return cmd
}

func send(ctx context.Context, sender *message.Sender, peer tg.InputPeerClass, m, username string) error {
	_, err := sender.To(peer).Text(ctx, m)
	if err == nil {
		return nil
	}

	if strings.Contains(err.Error(), "PEER_ID_INVALID") && len(username) > 0 {
		peer, err := resolve(ctx, sender, username)
		if err != nil {
			return err
		}

		return send(ctx, sender, peer, m, "")
	}

	flood, err := tgerr.FloodWait(ctx, err)
	if flood {
		return send(ctx, sender, peer, m, username)
	}

	return err
}

func resolve(ctx context.Context, sender *message.Sender, username string) (tg.InputPeerClass, error) {
	peer, err := sender.Resolve(username).AsInputPeer(ctx)
	if err == nil {
		return peer, nil
	}

	flood, err := tgerr.FloodWait(ctx, err)
	if flood {
		return resolve(ctx, sender, username)
	}

	return nil, err
}
