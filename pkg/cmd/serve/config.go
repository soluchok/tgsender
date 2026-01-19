package serve

import "errors"

type config struct {
	AppID      int    `mapstructure:"app-id"`
	AppHash    string `mapstructure:"app-hash"`
	BotToken   string `mapstructure:"bot-token"`
	ListenAddr string `mapstructure:"listen-addr"`
	StaticDir  string `mapstructure:"static-dir"`
}

func (c *config) Validate() error {
	if c.AppID == 0 {
		return errors.New("Telegram's app_id for authentication is missing.")
	}

	if len(c.AppHash) == 0 {
		return errors.New("Telegram's app_hash for authentication is missing.")
	}

	if len(c.BotToken) == 0 {
		return errors.New("Telegram's bot_token for OAuth authentication is missing.")
	}

	return nil
}
