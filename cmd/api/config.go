package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/victorzix/vhook/internal/errs"
)

// config holds the whole environment surface of the api: two infrastructure
// addresses, the bind address, two secrets and one local-infrastructure
// exception. Everything else — timeouts, shard count, backoff profiles, plan
// limits — is code or a database column, never an environment variable. See
// ARCHITECTURE.md §4.25.
type config struct {
	databaseURL   string
	rabbitURL     string
	httpAddr      string
	masterKey     []byte
	adminToken    string
	ssrfAllowlist []string
}

const defaultHTTPAddr = ":8080"

// masterKeyBytes selects AES-256, and matches what `adminctl genkey` prints.
const masterKeyBytes = 32

func loadConfig() (config, error) {
	cfg := config{
		databaseURL: os.Getenv("DATABASE_URL"),
		rabbitURL:   os.Getenv("RABBITMQ_URL"),
		httpAddr:    os.Getenv("VHOOK_HTTP_ADDR"),
		adminToken:  os.Getenv("VHOOK_ADMIN_TOKEN"),
	}
	encodedKey := os.Getenv("VHOOK_MASTER_KEY")

	// Every message below names the variable and never quotes its value: the
	// operator needs to know which one is wrong, and a secret must not reach a
	// terminal, a log or a CI transcript.
	var missing []string
	if cfg.databaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if cfg.rabbitURL == "" {
		missing = append(missing, "RABBITMQ_URL")
	}
	// Without these two the process must die before the port opens: a
	// management surface left open by an empty variable, and a secret column
	// nobody could read back, are both worse than refusing to boot.
	if cfg.adminToken == "" {
		missing = append(missing, "VHOOK_ADMIN_TOKEN")
	}
	if encodedKey == "" {
		missing = append(missing, "VHOOK_MASTER_KEY")
	}
	if len(missing) > 0 {
		return config{}, errors.Join(errs.MissingConfig,
			fmt.Errorf("config: missing %v", missing))
	}

	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return config{}, errors.Join(errs.MissingConfig,
			errors.New("config: VHOOK_MASTER_KEY is not valid base64"))
	}
	if len(key) != masterKeyBytes {
		return config{}, errors.Join(errs.MissingConfig,
			fmt.Errorf("config: VHOOK_MASTER_KEY decodes to %d bytes, want %d",
				len(key), masterKeyBytes))
	}
	cfg.masterKey = key

	cfg.ssrfAllowlist = splitAllowlist(os.Getenv("VHOOK_SSRF_ALLOWLIST"))

	if cfg.httpAddr == "" {
		cfg.httpAddr = defaultHTTPAddr
	}
	return cfg, nil
}

// splitAllowlist reads the comma-separated hostnames that skip the address
// check. Surrounding spaces are trimmed because a host with a space would
// never match the host of a URL, and the typo would look like the allowlist
// being ignored.
func splitAllowlist(raw string) []string {
	var out []string
	for _, host := range strings.Split(raw, ",") {
		if host = strings.TrimSpace(host); host != "" {
			out = append(out, host)
		}
	}
	return out
}
