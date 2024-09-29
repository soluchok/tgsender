package dump

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/gotd/td/examples"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/soluchok/tgsender/pkg/model"
	"github.com/soluchok/tgsender/pkg/session"
	"github.com/soluchok/tgsender/pkg/slices"
)

const (
	flagAppIDName  = "app-id"
	flagAppIDUsage = "Telegram's APP id (required)"

	flagAppHashName  = "app-hash"
	flagAppHashUsage = "Telegram's APP hash (required)"

	flagAuthName      = "auth"
	flagAuthShorthand = "a"
	flagAuthUsage     = "Telegram's phone number for authentication (required)"

	flagOutputName      = "output"
	flagOutputShorthand = "o"
	flagOutputValue     = "dump.out"
	flagOutputUsage     = "file name for the output data"
)

func New() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "dump",
		Short: "Download all of the user's contacts.",
		PreRun: func(cmd *cobra.Command, args []string) {
			viper.BindPFlag(flagAuthName, cmd.PersistentFlags().Lookup(flagAuthName))
			viper.BindPFlag(flagAppIDName, cmd.PersistentFlags().Lookup(flagAppIDName))
			viper.BindPFlag(flagAppHashName, cmd.PersistentFlags().Lookup(flagAppHashName))
			viper.BindPFlag(flagOutputName, cmd.PersistentFlags().Lookup(flagOutputName))
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			var cfg *config
			if err := errors.Join(viper.Unmarshal(&cfg), cfg.Validate()); err != nil {
				return err
			}

			_, err := os.Stat(cfg.Output)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("failed to check output file: %w", err)
			}

			if err == nil {
				slog.Warn("The output file already exists, so the data will be appended to the existing file.", slog.String("file", cfg.Output))
			}

			out, err := os.OpenFile(cfg.Output, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
			if err != nil {
				return err
			}

			defer out.Close()

			store, err := session.Get(cfg.Authentication)
			if err != nil {
				return fmt.Errorf("failed to get session: %w", err)
			}

			var client = telegram.NewClient(cfg.AppID, cfg.AppHash, telegram.Options{SessionStorage: store})
			var flow = auth.NewFlow(examples.Terminal{PhoneNumber: cfg.Authentication}, auth.SendCodeOptions{})

			return client.Run(cmd.Context(), func(ctx context.Context) error {
				if err := client.Auth().IfNecessary(ctx, flow); err != nil {
					return err
				}

				resp, err := client.API().ContactsGetContacts(ctx, 0)
				if err != nil {
					return fmt.Errorf("failed to get contacts: %w", err)
				}

				contacts, ok := resp.AsModified()
				if !ok {
					return nil
				}

				for _, user := range slices.Convert(contacts.GetUsers(), toUser) {
					if err := json.NewEncoder(out).Encode(user); err != nil {
						return fmt.Errorf("failed to encoder user: %w", err)
					}
				}

				return err
			})
		},
	}

	cmd.PersistentFlags().StringP(flagAuthName, flagAuthShorthand, "", flagAuthUsage)
	cmd.PersistentFlags().Int(flagAppIDName, 0, flagAppIDUsage)
	cmd.PersistentFlags().String(flagAppHashName, "", flagAppHashUsage)
	cmd.PersistentFlags().StringP(flagOutputName, flagOutputShorthand, flagOutputValue, flagOutputUsage)

	return cmd
}

func toInputPhoneContacts(val string) tg.InputPhoneContact {
	return tg.InputPhoneContact{Phone: val}
}

func toUser(val tg.UserClass, _ int) model.User {
	user, ok := val.AsNotEmpty()
	if !ok {
		return model.User{}
	}

	return model.User{
		ID:         user.ID,
		AccessHash: user.AccessHash,
		FirstName:  user.FirstName,
		LastName:   user.LastName,
		Username:   user.Username,
		Phone:      user.Phone,
	}
}

func fileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return !errors.Is(err, os.ErrNotExist)
}
