package check

import "testing"

// Section 15.7: a proof is a data class with a marked constructor and
// a marked Tx. Its transaction is used only before the dot, and an
// action fun takes one only from an api list and passes none on.
func TestProofs(t *testing.T) {
	model := "package kogo.rpc\nexternal class Request\nexternal class Bell\naction external fun Bell.ring()\n---\n" +
		"@file:OptIn(M::class)\npackage t\nimport kogo.rpc.Request\nimport kogo.rpc.Bell\n@RequiresOptIn\nannotation class M\n" +
		"data class Env(val bell: Bell)\ndata class AtTenant @M constructor(@property:M val tx: Tx)\n" +
		"data class Held @M constructor(val id: Int, val tenant: AtTenant, @property:M val tx: Tx)\n" +
		"data class Two @M constructor(val other: Tx, @property:M val tx: Tx)\n" +
		"interface OnBooking {\n    val id: Int\n    @M val tx: Tx\n}\n" +
		"fun gate(tx: Tx, env: Env, request: Request): Either<String, Held> = either { Held(1, AtTenant(tx), tx) }\n" +
		"fun save(booking: Held, n: Int): Int = booking.tx.execute(sql\"${booking.id}: Int, $n: Int\")\n"
	passes(t,
		// A statement on a proof's transaction need bind nothing of the proof.
		model+"fun deletePassenger(booking: Held, passengerId: Int): Boolean =\n    booking.tx.execute(sql\"$passengerId: Int\") == 1",
		model+"fun places(at: AtTenant): Int = at.tx.execute(sql\"\")\nfun f(booking: Held): Int = booking.tenant.tx.execute(sql\"\")",
		// Making a thing gives its proof: the transaction goes into a proof's own.
		model+"fun insert(booking: Held, id: Int): Held = Held(id, booking.tenant, booking.tx)\nfun wider(booking: Held): AtTenant = AtTenant(booking.tx)",
		model+"fun f(two: Two, booking: Held): Two = two.copy(tx = booking.tx)",
		// A _ takes nothing out of a proof.
		model+"fun f(booking: Held): Int {\n    val (id, tenant, _) = booking\n    return id\n}",
		// Proofs share an interface, held as they are, and a proof implements it where the file opts in.
		model+"data class Real @M constructor(override val id: Int, @property:M override val tx: Tx) : OnBooking",
		model+"fun f(on: OnBooking): Boolean = on.tx.exists(sql\"${on.id}: Int\")",
		// A data class that is no proof is held to none of it.
		model+"data class Plain(val n: Int, val tx: Tx)\nfun f(p: Plain): Tx {\n    p.tx.execute(sql\"\")\n    return p.tx\n}",
		// A gate gives a proof, or facts alone (spec.md 15.2).
		model+"data class Facts(val n: Int, val at: AtTenant)\nfun g(tx: Tx, env: Env, request: Request): Either<String, Facts> = either { Facts(1, AtTenant(tx)) }\n"+
			"fun w(env: Env, f: Facts, input: Unit): Either<String, Int> = either { f.n }\napi a unchecked { w gatedBy g }",
		model+"data class Facts(val held: List<Held>)\nfun g(tx: Tx, env: Env, request: Request): Either<String, Facts> = either { Facts(listOf()) }\n"+
			"fun w(env: Env, f: Facts, input: Unit): Either<String, Int> = either { f.held.size }\napi a unchecked { w gatedBy g }",
		// A step and work that runs outside read the proof's facts.
		model+"api a unchecked {\n    work gatedBy gate after step\n    out gatedBy gate\n}\n"+
			"action fun step(env: Env, booking: Held, input: Unit): Either<String, Unit> = either {\n    ensure(booking.id > 0) { \"no\" }\n    env.bell.ring()\n}\n"+
			"fun work(env: Env, booking: Held, input: Unit): Either<String, Int> = either { save(booking, 1) }\n"+
			"action fun out(env: Env, booking: Held, input: Unit): Either<String, Int> = either {\n    env.bell.ring()\n    booking.id\n}",
	)
	step := model + "api a unchecked { work gatedBy gate after step }\nfun work(env: Env, booking: Held, input: Unit): Either<String, Int> = either { save(booking, 1) }\n" +
		"action fun step(env: Env, booking: Held, input: Unit): Either<String, Unit> = either {\n    env.bell.ring()\n"
	refused(t, [][3]string{
		// The design's refused samples (doc/gates.md).
		{model + "fun deletePassenger(tx: Tx, passengerId: Int): Int = tx.execute(sql\"$passengerId: Int\")\nfun removePassenger(booking: Held, passengerId: Int): Int =\n    ‸deletePassenger(booking.tx, passengerId)", "proof-tx-escapes", "booking.tx is passed to deletePassenger; a proof's transaction is used only as booking.tx.query/execute/...: pass booking instead"},
		{step + "    ‸save(booking, 1)\n}", "outside-passes-proof", "step passes booking to save, and an outside step has no transaction: move the call to the work"},
		// The transaction leaves by no other way.
		{model + "fun f(booking: Held): Int {\n    val tx = ‸booking.tx\n    return 1\n}", "proof-tx-escapes", "booking.tx is kept or handed on here; a proof's transaction is used only as booking.tx.query/execute/...: keep and pass booking instead"},
		{model + "fun f(booking: Held): Tx = ‸booking.tx", "proof-tx-escapes", "booking.tx is kept or handed on here"},
		{model + "data class Plain(val tx: Tx)\nfun f(booking: Held): Plain = ‸Plain(booking.tx)", "proof-tx-escapes", "booking.tx is passed to Plain"},
		{model + "fun f(booking: Held): Int {\n    ‸val (id, tenant, tx) = booking\n    return id\n}", "proof-tx-escapes", "this takes tx out of a Held"},
		// It fills a proof's own transaction, and no other property.
		{model + "fun f(booking: Held): Two = ‸Two(booking.tx, booking.tx)", "proof-tx-escapes", "booking.tx is passed to Two"},
		{model + "fun f(two: Two, booking: Held): Two = ‸two.copy(other = booking.tx)", "proof-tx-escapes", "booking.tx is passed to copy"},
		// An action fun takes a proof only from an api list, and uses none.
		{model + "action fun f(env: Env, ‸booking: Held?) {\n    env.bell.ring()\n}", "action-proof", "f is an action fun and takes a proof, booking"},
		{model + "action fun f(env: Env, ‸booking: Held) {\n    env.bell.ring()\n}", "action-proof", "f is an action fun and takes a proof, booking: an action runs with no transaction open, and a proof is handed to one only by an api list"},
		{step + "    ‸booking.tx.execute(sql\"${booking.id}: Int\")\n}", "outside-passes-proof", "step runs a statement on booking.tx, and an outside step has no transaction: that one has ended, so move the statement to the work"},
		{step + "    ‸places(booking.tenant)\n}\nfun places(at: AtTenant): Int = at.tx.execute(sql\"\")", "outside-passes-proof", "step passes booking.tenant to places"},
		{step + "    ‸booking.show()\n}\nfun Held.show(): Int = tx.execute(sql\"${id}: Int\")", "outside-passes-proof", "step passes booking to show"},
		{step + "    ‸retry { booking }\n}\nfun retry(f: () -> Held): Int = save(f(), 1)", "outside-passes-proof", "step passes a proof to retry"},
		{model + "api a unchecked { out gatedBy gate }\naction fun out(env: Env, booking: Held, input: Unit): Either<String, Int> = either {\n    env.bell.ring()\n    ‸save(booking, 1)\n}", "outside-passes-proof",
			"out passes booking to save, and work that runs outside has no transaction: move the call into the gate, or make out a fun, which runs in the transaction"},
		// A gate that gives a transaction gives it in a proof.
		{model + gives("data class Clerk(val n: Int, val tx: SerializableTx)"), "gate-proof", "g gives Clerk, which holds a transaction, tx, in Clerk, which is no proof: mark the constructor and the transaction of Clerk"},
		{model + gives("data class Box(val n: Int, val tx: Tx)\ndata class Clerk(val n: Int, val box: Box?)"), "gate-proof", "holds a transaction, box.tx, in Box, which is no proof: mark the constructor and the transaction of Box"},
		{model + gives("interface Clerk {\n    val tx: Tx\n}"), "gate-proof", "tx, in Clerk, which is no proof: mark it with a marker the gates' file opts in to, as @M val tx: Tx"},
		// ... at any depth: a type argument, a sealed variant, a function's result, a proof's other Tx.
		{model + gives("data class Plain(val n: Int, val tx: Tx)\ndata class Clerk(val xs: List<Plain>)"), "gate-proof", "xs.tx, in Plain, which is no proof"},
		{model + gives("data class Box<T>(val v: T)\ntypealias Clerk = Box<Tx>"), "gate-proof", "g gives Box<Tx>, which holds a transaction, Tx, outside any proof"},
		{model + gives("sealed interface Clerk {\n    data class V(val tx: Tx) : Clerk\n}"), "gate-proof", "tx, in Clerk.V, which is no proof"},
		{model + gives("data class Clerk(val f: () -> Tx)"), "gate-proof", "holds a transaction, f(), outside any proof: hold it as a proof's marked transaction, or give the facts alone"},
		{model + gives("typealias Clerk = Two"), "gate-proof", "g gives Two, which holds a transaction, other, and Two is a proof that holds it outside its marked one: a proof holds one transaction, so take other out of Two"},
		// A marked property is read, and overridden, only where the file opts in (section 11).
		{model + "---\ndata class Fake(override val id: Int, ‸override val tx: Tx) : OnBooking", "opt-in", "tx is marked @M: begin this file with @file:OptIn(M::class)"},
		{model + "interface Shown {\n    @M fun show(): Int\n}\n---\ndata class S(val n: Int) : Shown {\n    override ‸fun show(): Int = n\n}", "opt-in", "show is marked @M"},
		{model + "---\nfun f(booking: Held): Int {\n    ‸val (id, tenant, _) = booking\n    return id\n}", "opt-in", "tx is marked @M"},
		{model + "---\nfun f(booking: Held): Int = booking.id + booking.‸tx.execute(sql\"${booking.id}: Int\")", "opt-in", "tx is marked @M: begin this file with @file:OptIn(M::class)"},
	})
}

// gives is a gate g that gives the Clerk that decl declares, and a
// row that names it.
func gives(decl string) string {
	return decl + "\nfun g(tx: Tx, env: Env, request: Request): Either<String, Clerk> = either { raise(\"no\") }\n" +
		"fun w(env: Env, c: Clerk, input: Unit): Either<String, Int> = either { 1 }\napi a unchecked { w gatedBy ‸g }"
}
