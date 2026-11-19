package check

import "testing"

// Section 7.1: a fun that calls an action is an action fun, which
// takes no Tx, acts in no block that holds one, and is not kept in a
// value.
func TestActions(t *testing.T) {
	head := "package kogo.io\nexternal class Bell\naction external fun Bell.ring()\nexternal fun Bell.heard(): Boolean\n---\nimport kogo.io.Bell\n" +
		"action fun f(b: Bell) {\n    b.ring()\n}\n"
	passes(t,
		// The word is carried up, and a binding that says nothing is allowed anywhere.
		head+"action fun g(b: Bell) = f(b)\nfun h(tx: Tx, b: Bell): Boolean = b.heard()",
		// It acts between its transactions, and in a block of the prelude's.
		head+"action fun g(db: Db, b: Bell): Either<String, Int> = either {\n    val n = db.tx { tx -> tx.execute(sql\"\") }.bind()\n    f(b)\n    n\n}",
		// A local fun's body does not end the lambdas of the fun around it.
		head+"action fun g(b: Bell): Either<String, Int> = either {\n    f(b)\n    fun local(): Int = 1\n    local()\n}",
	)
	refused(t, [][3]string{
		{head + "fun g(b: Bell) = ‸f(b)", "unmarked-action", "g calls f, an action: make g an action fun, or move the call"},
		{head + "fun g(b: Bell) {\n    b.‸ring()\n}", "unmarked-action", "g calls ring, an action: make g an action fun"},
		{head + "val Bell.rung: Unit\n    get() = ‸f(this)", "unmarked-action", "f is an action, and only an action fun calls one: move the call into an action fun"},
		{head + "action ‸fun g(b: Bell): Boolean = b.heard()", "idle-action", "g is an action fun and calls no action: leave the word out, fun g"},
		{head + "fun g(b: Bell): Int {\n    fun h(): Int {\n        ‸f(b)\n        return 1\n    }\n    return h()\n}", "unmarked-action", "h calls f, an action, and only a top-level fun is an action fun: move the call out of h, into the action fun around it"},
		{head + "action fun g(b: Bell) {\n    b.‸ring(1)\n}", "arguments", "ring takes 0 arguments"},
		{head + "action ‸fun Tx.g(b: Bell) = f(b)", "action-tx", "g is an action fun on a Tx"},
		{head + "action fun g(‸tx: Tx, b: Bell) = f(b)", "action-tx", "g is an action fun and takes a Tx, tx: an action runs with no transaction open, so take a Db"},
		{head + "action fun g(‸tx: SerializableTx, b: Bell) = f(b)", "action-tx", "g is an action fun and takes a Tx, tx"},
		{head + "action fun g(db: Db, b: Bell): Int = db.tx(serializable = true) { tx ->\n    ‸f(b)\n    1\n}", "action-in-tx", "f is an action, called in a block that holds a Tx"},
		{head + "action fun g(db: Db, b: Bell): Int = db.tx { tx ->\n    ‸f(b)\n    1\n}", "action-in-tx", "f is an action, called in a block that holds a Tx: a transaction may roll back or run again and the action cannot, so move the call before or after the block"},
		{head + "action fun g(b: Bell) {\n    val h = ::‸f\n    f(b)\n}", "action-kept", "::f names an action fun, and an action is not kept in a value: call f where it is wanted"},
		{head + "fun later(block: () -> Unit) = block()\naction fun g(b: Bell) = later ‸{ f(b) }", "action-kept", "this lambda calls f, an action, and is not passed straight to a fun of the prelude"},
		{head + "action fun g(b: Bell) {\n    val h = ‸{ f(b) }\n    h()\n}", "action-kept", "this lambda calls f, an action"},
		{head + "interface I {\n    action ‸fun act()\n}", "action-kept", "act is a member of I, and a member is not an action fun"},
	})
}
