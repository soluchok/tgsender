package check

import "errors"

type config struct {
	AppID          int      `mapstructure:"app-id"`
	AppHash        string   `mapstructure:"app-hash"`
	Authentication string   `mapstructure:"auth"`
	Output         string   `mapstructure:"output"`
	Input          string   `mapstructure:"input"`
	Retry          string   `mapstructure:"retry"`
	Phones         []string `mapstructure:"phones"`
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

	if len(c.Authentication) == 0 {
		return errors.New("Telegram's phone number for authentication is missing.")
	}

	if len(c.Phones) == 0 && len(c.Input) == 0 {
		return errors.New("Nothing to check, phones were not provided.")
	}

	return nil
}
