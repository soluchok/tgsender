package root

import (
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/soluchok/tgsender/pkg/cmd/check"
	"github.com/soluchok/tgsender/pkg/cmd/dump"
	"github.com/soluchok/tgsender/pkg/cmd/send"
	"github.com/soluchok/tgsender/pkg/cmd/serve"
)

func New() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "tgsender",
		Short: "tgsender is an application used to verify if a phone number is registered on Telegram and to send messages to those users.",
	}

	cmd.AddCommand(check.New())
	cmd.AddCommand(send.New())
	cmd.AddCommand(dump.New())
	cmd.AddCommand(serve.New())

	return cmd
}

func Execute() {
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	if err := New().Execute(); err != nil {
		slog.Debug("root cmd failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
