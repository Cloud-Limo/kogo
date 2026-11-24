// Package testdb gives a test's driver a schema of its own in the
// test database. A transaction commits (spec.md 10.7), and test
// packages run side by side: in one schema each would read the rows
// the others commit.
package testdb

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Open makes a schema from the statements of schemaFile, which is
// testdata/schema.sql, in the database $DATABASE_URL names, and gives
// a pool whose connections find it first. A statement checked against
// the tables of public then runs against the schema's. drop removes
// it and closes the pool.
func Open(ctx context.Context, schemaFile string) (pool *pgxpool.Pool, drop func()) {
	must := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	statements, err := os.ReadFile(schemaFile)
	must(err)
	cfg, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	must(err)
	schema := fmt.Sprintf("test_%d", os.Getpid())
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ", public"
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC" // as kogo.CheckPool requires
	pool, err = pgxpool.NewWithConfig(ctx, cfg)
	must(err)
	_, err = pool.Exec(ctx, "drop schema if exists "+schema+" cascade; create schema "+schema+"; "+string(statements))
	must(err)
	return pool, func() {
		pool.Exec(ctx, "drop schema "+schema+" cascade")
		pool.Close()
	}
}
