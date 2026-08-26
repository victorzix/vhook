package obs_test

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/victorzix/vhook/internal/obs"
)

func TestLoggerWritesJSON(t *testing.T) {
	var out strings.Builder
	obs.NewLogger(&out, slog.LevelInfo).Info("subiu", "port", 8080)

	var line map[string]any
	if err := json.Unmarshal([]byte(out.String()), &line); err != nil {
		t.Fatalf("log não é JSON: %v — %q", err, out.String())
	}
	if line["msg"] != "subiu" {
		t.Errorf("msg = %v, want subiu", line["msg"])
	}
}

func TestClientCorrelationIDIsLoggedSeparately(t *testing.T) {
	var out strings.Builder
	logger := obs.NewLogger(&out, slog.LevelInfo)

	obs.LogRequest(logger, "cliente-123", true).Info("requisição")

	var line map[string]any
	if err := json.Unmarshal([]byte(out.String()), &line); err != nil {
		t.Fatalf("log não é JSON: %v", err)
	}
	if line["client_correlation_id"] != "cliente-123" {
		t.Errorf("client_correlation_id = %v", line["client_correlation_id"])
	}
}

func TestInvalidClientCorrelationIDIsMarkedAsDropped(t *testing.T) {
	var out strings.Builder
	logger := obs.NewLogger(&out, slog.LevelInfo)

	obs.LogRequest(logger, strings.Repeat("x", 65), false).Info("requisição")

	var line map[string]any
	if err := json.Unmarshal([]byte(out.String()), &line); err != nil {
		t.Fatalf("log não é JSON: %v", err)
	}
	if line["client_correlation_id_dropped"] != true {
		t.Error("valor inválido devia ter sido marcado como descartado")
	}
	if _, present := line["client_correlation_id"]; present {
		t.Error("valor inválido não pode aparecer no log")
	}
}
