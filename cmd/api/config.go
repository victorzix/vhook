package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/victorzix/vhook/internal/errs"
)

// config holds the whole environment surface of the api: two infrastructure
// addresses and the bind address. Everything else — timeouts, shard count,
// backoff profiles — is code or a database column, never an environment
// variable. See ARCHITECTURE.md §4.25.
type config struct {
	databaseURL string
	rabbitURL   string
	httpAddr    string
}

const defaultHTTPAddr = ":8080"

func loadConfig() (config, error) {
	cfg := config{
		databaseURL: os.Getenv("DATABASE_URL"),
		rabbitURL:   os.Getenv("RABBITMQ_URL"),
		httpAddr:    os.Getenv("VHOOK_HTTP_ADDR"),
	}

	var missing []string
	if cfg.databaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if cfg.rabbitURL == "" {
		missing = append(missing, "RABBITMQ_URL")
	}
	if len(missing) > 0 {
		return config{}, errors.Join(errs.MissingConfig,
			fmt.Errorf("config: missing %v", missing))
	}

	if cfg.httpAddr == "" {
		cfg.httpAddr = defaultHTTPAddr
	}
	return cfg, nil
}
