package test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Run refuses a pool not in UTC before any test runs, so tests run on
// a pool the server would take (spec.md 15.5, the database handle).
func TestRunChecksPool(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://kogo:kogo@127.0.0.1/kogo_test") // it connects when first asked
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	defer func() {
		if p := fmt.Sprint(recover()); !strings.Contains(p, "timezone") {
			t.Errorf("Run takes a pool not in UTC, or refuses it with %s", p)
		}
	}()
	Run(context.Background(), pool, io.Discard)
}
