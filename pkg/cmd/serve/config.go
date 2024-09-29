package serve

import "errors"

type config struct {
	AppID   int    `mapstructure:"app-id"`
	AppHash string `mapstructure:"app-hash"`
}

func (c *config) Validate() error {
	if c == nil {
		return errors.New("The configuration is missing. Please ensure that it was properly parsed.")
	}

	if c.AppID == 0 {
		return errors.New("Telegram's app_id for authentication is missing.")
	}

	if len(c.AppHash) == 0 {
		return errors.New("Telegram's app_hash for authentication is missing.")
	}

	return nil
}
