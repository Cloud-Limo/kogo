package check

import "testing"

// A gate only reads (spec.md 15.3): each statement it reaches, in a
// fun it calls, a local fun, a lambda or a fun it names too, needs
// select alone of each table, and a lock is a SerializableTx's to
// refuse.
func TestGateWrites(t *testing.T) {
	rest := `data class P(val n: Int)
fun plain(tx: Tx, env: Env, request: Request): Either<String, P> = either { P(seen(tx)) }
fun v(env: Env, p: P, input: Unit): Either<String, Unit> = either { }
fun seen(tx: Tx): Int = tx.execute(sql"@person @session:lock")
`
	head := rpc() + rest
	work := "fun w(tx: Tx, env: Env, request: Request, input: Unit): Either<String, Unit> = either { tx.execute(sql\"@person:update\") }\n"
	passes(t,
		// A gate on a Tx may lock, a statement that no gate reaches
		// writes, and so does an ungated operation.
		head+work+"api ops unchecked { v gatedBy plain, w ungated }",
	)
	refused(t, [][3]string{
		{head + "fun g(tx: SerializableTx, env: Env, request: Request): Either<String, P> = either { P(tx.execute(‸sql\"@person @session:update\")) }\napi ops unchecked { v gatedBy g }",
			"gate-writes", "this statement updates session, and a gate only reads: move it to the work (the gate g reaches it"},
		{head + "fun g(tx: Tx, env: Env, request: Request): Either<String, P> = either { P(tx.execute(‸sql\"@person:insert\")) }\napi ops unchecked { v gatedBy g }",
			"gate-writes", "this statement inserts into person, and a gate only reads"},
		{head + "fun g(tx: SerializableTx, env: Env, request: Request): Either<String, P> = either { P(tx.execute(‸sql\"@session:lock\")) }\napi ops unchecked { v gatedBy g }",
			"gate-writes", "this statement locks session, and a gate on a SerializableTx only reads: lock it in the work"},
		// Through a fun it calls, in another file and reached by two
		// rows: said once, where the statement is.
		{rpc("import m.gone\n") + rest + "fun g(tx: SerializableTx, env: Env, request: Request): Either<String, P> = either { P(gone(tx)) }\napi ops unchecked { v gatedBy g }\napi more unchecked { v gatedBy g }\n---\npackage m\nfun gone(tx: Tx): Int = tx.execute(‸sql\"@session:delete\")",
			"gate-writes", "this statement deletes from session, and a gate only reads: move it to the work (the gate g reaches it"},
		// Through a local fun, a lambda and a fun named as a value.
		{head + "fun g(tx: SerializableTx, env: Env, request: Request): Either<String, P> = either {\n    fun inner(): Int = tx.execute(‸sql\"@person:update\")\n    P(inner())\n}\napi ops unchecked { v gatedBy g }",
			"gate-writes", "updates person"},
		{head + "fun g(tx: SerializableTx, env: Env, request: Request): Either<String, P> = either { P(listOf(1).map { n -> tx.execute(‸sql\"@person:update\") }.size) }\napi ops unchecked { v gatedBy g }",
			"gate-writes", "updates person"},
		{head + "fun up(tx: Tx): Int = tx.execute(‸sql\"@person:update\")\nfun g(tx: SerializableTx, env: Env, request: Request): Either<String, P> = either {\n    val f = ::up\n    P(f(tx))\n}\napi ops unchecked { v gatedBy g }",
			"gate-writes", "updates person"},
	})
}
