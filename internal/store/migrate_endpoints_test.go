package store_test

import (
	"context"
	"testing"

	"github.com/victorzix/vhook/internal/store"
)

func TestMigrateCreatesTheEndpointURLIndex(t *testing.T) {
	ctx := context.Background()
	url := startPostgres(t)

	if err := store.Migrate(ctx, url); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	if !indexNames(t, ctx, url)["endpoints_application_url_idx"] {
		t.Error("falta o índice endpoints_application_url_idx")
	}
}
