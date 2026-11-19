// The test main of TestKogoTest: what an application writes in Go to
// run its Kogo tests. It makes the pool and the environment and hands
// it to the runner, which runs every tests list linked in. ROOT stands
// for the import path of the directory the Go is written to.
package main

import (
	"context"
	"os"

	"ROOT/example/ops"
	"github.com/Cloud-Limo/kogo/internal/testdb"
	"github.com/Cloud-Limo/kogo/prelude/kogo/test"
)

func main() {
	ctx := context.Background()
	pool, drop := testdb.Open(ctx, "../../../testdata/schema.sql")
	failed := test.Run(ctx, pool, os.Stdout, ops.OpsEnv{MaxLooks: 2})
	drop()
	if failed > 0 {
		os.Exit(1)
	}
}
