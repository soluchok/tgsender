package check

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/gotd/td/examples"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
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

	flagPhonesName      = "phones"
	flagPhonesShorthand = "p"
	flagPhonesUsage     = "list of comma-separated phones that need to be verified (required)"

	flagOutputName      = "output"
	flagOutputShorthand = "o"
	flagOutputValue     = "users.out"
	flagOutputUsage     = "file name for the output data"

	flagRetryName      = "retry"
	flagRetryShorthand = "r"
	flagRetryValue     = "retry.out"
	flagRetryUsage     = "file name for the retry data"

	flagInputName  = "input"
	flagInputUsage = "input's data file name"
)

func New() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "check",
		Short: "Verifies whether a phone numbers are registered on Telegram",
		PreRun: func(cmd *cobra.Command, args []string) {
			viper.BindPFlag(flagPhonesName, cmd.PersistentFlags().Lookup(flagPhonesName))
			viper.BindPFlag(flagAuthName, cmd.PersistentFlags().Lookup(flagAuthName))
			viper.BindPFlag(flagAppIDName, cmd.PersistentFlags().Lookup(flagAppIDName))
			viper.BindPFlag(flagAppHashName, cmd.PersistentFlags().Lookup(flagAppHashName))
			viper.BindPFlag(flagOutputName, cmd.PersistentFlags().Lookup(flagOutputName))
			viper.BindPFlag(flagInputName, cmd.PersistentFlags().Lookup(flagInputName))
			viper.BindPFlag(flagRetryName, cmd.PersistentFlags().Lookup(flagRetryName))
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

			retry, err := os.Create(cfg.Retry)
			defer retry.Close()

			var uniquePhoneSet = map[string]struct{}{}
			var savedPhoneSet = map[string]struct{}{}

			in, err := os.OpenFile(cfg.Input, os.O_RDWR, 0666)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}

			if errors.Is(err, os.ErrNotExist) && len(cfg.Input) > 0 {
				return err
			}

			defer in.Close()

			var readers []io.Reader
			if err == nil {
				readers = append(readers, in)
			}

			if len(cfg.Phones) > 0 {
				readers = append(readers, strings.NewReader(strings.Join(cfg.Phones, "\n")+"\n"))
			}

			var scanner = bufio.NewScanner(io.MultiReader(readers...))

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

				contactsResp, err := client.API().ContactsGetContacts(ctx, 0)
				if err != nil {
					return fmt.Errorf("failed to get contacts: %w", err)
				}

				contacts := contactsResp.(*tg.ContactsContacts).GetUsers()

				var phones []string
				for scanner.Scan() {
					phone := scanner.Text()
					if len(phone) == 0 {
						continue
					}

					if _, ok := uniquePhoneSet[phone]; ok {
						continue
					}

					uniquePhoneSet[phone] = struct{}{}

					phones = append(phones, phone)

					for len(phones) > 15 {
						if err := ImportContactsAndSave(ctx, client.API(), out, retry, phones[:15], savedPhoneSet, contacts); err != nil {
							return err
						}

						phones = phones[15:]
					}
				}

				if err := ImportContactsAndSave(ctx, client.API(), out, retry, phones, savedPhoneSet, contacts); err != nil {
					return err
				}

				return scanner.Err()
			})
		},
	}

	cmd.PersistentFlags().StringSliceP(flagPhonesName, flagPhonesShorthand, nil, flagPhonesUsage)
	cmd.PersistentFlags().StringP(flagAuthName, flagAuthShorthand, "", flagAuthUsage)
	cmd.PersistentFlags().Int(flagAppIDName, 0, flagAppIDUsage)
	cmd.PersistentFlags().String(flagAppHashName, "", flagAppHashUsage)
	cmd.PersistentFlags().StringP(flagOutputName, flagOutputShorthand, flagOutputValue, flagOutputUsage)
	cmd.PersistentFlags().String(flagInputName, "", flagInputUsage)
	cmd.PersistentFlags().StringP(flagRetryName, flagRetryShorthand, flagRetryValue, flagRetryUsage)

	return cmd
}

func ImportContactsAndSave(ctx context.Context, api *tg.Client, out, retry io.Writer, phones []string, phoneSet map[string]struct{}, contacts []tg.UserClass) error {
	resp, err := ImportContacts(ctx, api, phones)
	if err != nil {
		return fmt.Errorf("failed to import contacts: %w", err)
	}

	for _, id := range resp.GetRetryContacts() {
		retry.Write([]byte(phones[id]))
		retry.Write([]byte{'\n'})
	}

	for _, user := range slices.Convert(resp.GetUsers(), toUser) {
		if _, ok := phoneSet[user.Phone]; ok {
			continue
		}

		phoneSet[user.Phone] = struct{}{}

		if err := json.NewEncoder(out).Encode(user); err != nil {
			return fmt.Errorf("failed to encoder user: %w", err)
		}
	}

	var toDelete []tg.InputUserClass
	for _, iu := range resp.GetUsers() {
		u, ok := iu.AsNotEmpty()
		if !ok {
			continue
		}

		toDelete = append(toDelete, &tg.InputUser{
			UserID:     u.ID,
			AccessHash: u.AccessHash,
		})

		for _, cu := range contacts {
			if iu.GetID() == cu.GetID() {
				toDelete = toDelete[:len(toDelete)-1]
				break
			}
		}
	}

	if len(toDelete) == 0 {
		return nil
	}

	if _, err = api.ContactsDeleteContacts(ctx, toDelete); err != nil {
		return fmt.Errorf("failed to delete contacts: %w", err)
	}

	return nil
}

func ImportContacts(ctx context.Context, api *tg.Client, phones []string) (*tg.ContactsImportedContacts, error) {
	res, err := api.ContactsImportContacts(ctx, slices.Convert(phones, toInputPhoneContacts))
	if err == nil {
		return res, nil
	}

	flood, err := tgerr.FloodWait(ctx, err)
	if flood {
		return ImportContacts(ctx, api, phones)
	}

	return nil, err
}

func toInputPhoneContacts(val string, id int) tg.InputPhoneContact {
	return tg.InputPhoneContact{Phone: val, ClientID: int64(id)}
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
