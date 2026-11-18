// The test main of TestKogoTest: what an application writes in Go to
// run its Kogo tests. It makes the pool and the environment and hands
// each tests list to the runner. ROOT stands for the import path of
// the directory the Go is written to, and LISTS for the lists run.
package main

import (
	"context"
	"os"

	"ROOT/example/ops"
	"kogo/internal/testdb"
	"kogo/prelude/kogo/test"
)

func main() {
	ctx := context.Background()
	pool, drop := testdb.Open(ctx, "../../../testdata/schema.sql")
	failed := test.Run(ctx, pool, ops.OpsEnv{MaxLooks: 2}, os.Stdout, LISTS)
	drop()
	if failed > 0 {
		os.Exit(1)
	}
}
