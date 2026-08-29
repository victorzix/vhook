package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"

	"github.com/victorzix/vhook/internal/errs"
)

// config is the whole environment surface of adminctl: one address and one
// secret. Everything else the command needs comes from flags, because it is
// behaviour of a single invocation and not of the deployment.
type config struct {
	databaseURL string
	masterKey   []byte
}

func loadConfig() (config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return config{}, errors.Join(errs.MissingConfig,
			errors.New("config: DATABASE_URL is not set"))
	}

	encoded := os.Getenv("VHOOK_MASTER_KEY")
	if encoded == "" {
		return config{}, errors.Join(errs.MissingConfig,
			errors.New("config: VHOOK_MASTER_KEY is not set — run `adminctl genkey`"))
	}

	// Errors never quote the value: the key must not reach a terminal, a log or
	// a CI transcript.
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return config{}, errors.Join(errs.MissingConfig,
			errors.New("config: VHOOK_MASTER_KEY is not valid base64"))
	}
	if len(key) != masterKeyBytes {
		return config{}, errors.Join(errs.MissingConfig,
			fmt.Errorf("config: VHOOK_MASTER_KEY decodes to %d bytes, want %d",
				len(key), masterKeyBytes))
	}

	return config{databaseURL: databaseURL, masterKey: key}, nil
}
