package session

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gotd/td/telegram"
)

const sessionDir = ".data"

func Get(phoneNumber string) (telegram.SessionStorage, error) {
	if err := os.MkdirAll(sessionDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create session directory: %w", err)
	}

	return &telegram.FileSessionStorage{
		Path: filepath.Join(sessionDir, phoneNumber+"_session.json"),
	}, nil
}
