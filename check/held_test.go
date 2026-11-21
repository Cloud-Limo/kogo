package check

import "testing"

// Section 15.7: a value holds its transaction in a plain property and
// passes it on; what outlives its transaction holds none, which is the
// answer of a gate whose work is an action fun, and of a db.tx block.
func TestHeldTransactions(t *testing.T) {
	model := "package kogo.rpc\nexternal class Request\nexternal class Bell\naction external fun Bell.ring()\n---\n" +
		"package t\nimport kogo.rpc.Request\nimport kogo.rpc.Bell\n" +
		"data class Env(val bell: Bell)\ndata class Person private constructor(val id: Int)\n" +
		"data class Call private constructor(val tx: SerializableTx, val person: Person)\n" +
		"data class Facts(val id: Int)\n" +
		"fun gate(tx: SerializableTx, env: Env, request: Request): Either<String, Call> = either { Call(tx, Person(1)) }\n" +
		"fun facts(tx: SerializableTx, env: Env, request: Request): Either<String, Facts> = either { Facts(gate(tx, env, request).bind().person.id) }\n" +
		"fun save(tx: Tx, person: Person, n: Int): Int = tx.execute(sql\"${person.id}: Int, $n: Int\")\n"
	work := "fun w(env: Env, call: Call, input: Unit): Either<String, Int> = either "
	out := "action fun out(env: Env, c: Clerk, input: Unit): Either<String, Unit> = either { env.bell.ring() }\n"
	passes(t,
		// A gate answers a data class with a plain SerializableTx and a
		// private constructor, and the work passes that transaction on.
		model+work+"{ save(call.tx, call.person, 1) }\napi a unchecked { w gatedBy gate }",
		// It is kept in a name, destructured to names and returned.
		model+work+"{\n    val tx = call.tx\n    val (held, person) = call\n    save(held, person, 1) + save(tx, call.person, 2)\n}\napi a unchecked { w gatedBy gate }",
		model+"fun txOf(call: Call): Tx = call.tx\nfun again(call: Call): Call = call.copy(tx = call.tx)",
		// Work that is an action fun takes the facts its gate answers.
		model+"api a unchecked { out gatedBy facts }\n"+
			"action fun out(env: Env, f: Facts, input: Unit): Either<String, Int> = either {\n    env.bell.ring()\n    f.id\n}",
		// A type that holds itself ends, and holds none.
		model+out+"data class Clerk(val n: Int, val next: Clerk?)\nfun g(tx: Tx, env: Env, request: Request): Either<String, Clerk> = either { raise(\"no\") }\napi a unchecked { out gatedBy g }",
		// A db.tx block answers facts, and a nested block does not end the transaction.
		model+"fun f(db: Db, env: Env, request: Request): Either<String, Int> = either {\n    val id = db.tx(serializable = true) { tx -> gate(tx, env, request).bind().person.id }.bind()\n    id\n}",
		model+"fun f(call: Call): Tx = call.tx.atomically { call.tx }",
	)
	refused(t, [][3]string{
		// A row whose work is an action fun names a gate whose answer holds no transaction.
		{model + "api a unchecked { out gatedBy ‸gate }\naction fun out(env: Env, call: Call, input: Unit): Either<String, Unit> = either { env.bell.ring() }", "action-gate",
			"out is an action fun, and its gate gate answers Call, which holds a transaction, tx, a property of Call: the server ends the gate's transaction before work that is an action fun runs, so name a gate that answers the facts out needs, or make out a fun, which runs in the transaction"},
		// ... at any depth: a nullable, a type argument, a sealed variant, a function's result.
		{model + out + gives("data class Clerk(val n: Int, val call: Call?)"), "action-gate", "answers Clerk, which holds a transaction, call.tx, a property of Call"},
		{model + out + gives("interface Clerk {\n    val tx: Tx\n}"), "action-gate", "tx, a property of Clerk"},
		{model + out + gives("data class Clerk(val xs: List<Call>)"), "action-gate", "xs.tx, a property of Call"},
		{model + out + gives("data class Box<T>(val v: T)\ntypealias Clerk = Box<Tx>"), "action-gate", "answers Box<Tx>, which holds a transaction, Tx:"},
		{model + out + gives("sealed interface Clerk {\n    data class V(val tx: Tx) : Clerk\n}"), "action-gate", "tx, a property of Clerk.V"},
		{model + out + gives("data class Clerk(val f: () -> Tx)"), "action-gate", "which holds a transaction, f():"},
		// A db.tx block answers nothing that holds a transaction.
		{model + "fun f(db: Db): Int {\n    val kept = db.‸tx(serializable = true) { tx -> tx }\n    return kept.execute(sql\"\")\n}", "tx-escapes",
			"this db.tx block answers SerializableTx, the transaction itself: the transaction ends with the block, so answer the facts the lines after it need"},
		{model + "fun f(db: Db): Tx = db.‸tx { tx -> tx }", "tx-escapes", "this db.tx block answers Tx, the transaction itself"},
		{model + "fun f(db: Db, env: Env, request: Request): Either<String, Call> = db.‸tx(serializable = true) { tx -> gate(tx, env, request).bind() }", "tx-escapes",
			"this db.tx block answers Call, which holds a transaction, tx, a property of Call: the transaction ends with the block"},
		{model + "data class Kept(val n: Int, val tx: Tx?)\nfun f(db: Db): Kept = db.‸tx { tx -> Kept(1, tx) }", "tx-escapes", "answers Kept, which holds a transaction, tx, a property of Kept"},
		// A private constructor is its file's: no other makes a call (section 2).
		{model + "---\npackage u\nimport t.Call\nimport t.Person\nfun f(tx: SerializableTx, p: Person): Call = ‸Call(tx, p)", "not-visible", "Call's constructor is private to f2.kg"},
	})
}

// gives is a gate g that answers the Clerk that decl declares, and a
// row whose work is the action fun out.
func gives(decl string) string {
	return decl + "\nfun g(tx: Tx, env: Env, request: Request): Either<String, Clerk> = either { raise(\"no\") }\n" +
		"api a unchecked { out gatedBy ‸g }"
}
