package check

import "testing"

// kt gives the few lines of kogo.test that tests need, and the head
// of a test file: its import and an environment.
const kt = "package kogo.test\nexternal class Test<E>\nexternal class Refusal\nexternal val Refusal.sqlState: String\n" +
	"external fun <E> test(sentence: String, body: (Tx, E) -> Unit): Test<E>\n---\n/*test*/import kogo.test.test\nimport kogo.test.Test\ndata class Env(val n: Int)\n"

// Section 16: what a tests list names, the type it gives each test,
// and what only a test file may write.
func TestTestLists(t *testing.T) {
	passes(t,
		// The list gives env its type, and tx.refusal a Refusal?.
		kt+"tests ts<Env> { a, b, }\nval a =\n    test(\"a\") { tx, env ->\n        require(env.n > 0) { \"n\" }\n    }\n"+
			"val b =\n    test(\"b\") { tx, env ->\n        val r = tx.refusal(sql\"\")\n        require(r?.sqlState == \"23505\") { \"r\" }\n    }",
		kt+"tests ts<Unit> { a }\nval a: Test<Unit> = test(\"a\") { tx, env -> }",
		// A test file's declarations, used by another test file.
		"/*test*/fun f(): Int = g()\n---\n/*test*/fun g(): Int = 1",
	)
	refused(t, [][3]string{
		{"data class Env(val n: Int)\n‸tests ts<Env> { a }", "test-file", "tests ts is in f1.kg: a tests list stands in a file named _test.kg"},
		{kt + "‸tests ts<Env> { }", "test", "tests ts names no test"},
		{kt + "tests ts<Env> { ‸nope }", "test", "nope is no top-level val of this file"},
		{kt + "tests ts<Env> { ‸b }\n---\n/*test*/val b: Int = 1", "test", "b is no top-level val of this file"},
		{kt + "tests ts<Env> { a, ‸a }\nval a = test(\"a\") { tx, env -> }", "duplicate", "a is already in a tests list"},
		{kt + "tests ts<Env> { ‸a }\nval a: Test<Int> = test(\"a\") { tx, env -> }", "test", "a is a Test<Int>, and tests ts runs a Test<Env>: leave its type out"},
		{kt + "tests ts<Env> { a }\nval a = test(\"a\") { tx, env -> env.‸m }", "unknown-name", "m"},
		{kt + "tests ts<Env> { a }\nval a = test(\"a\") { tx, env -> }\n‸val b = test(\"b\") { tx, env -> }", "val-type", "no tests list of this file names it: add it to the list"},
		{kt + "tests ts<Env> { a }\nval a = test(\"a\") { tx, env -> }\n‸val b: Test<Env> = test(\"b\") { tx, env -> }", "test", "b is a test that no tests list of this file names, so kogo test would never run it"},
		{"package kogo.test\nexternal class Test<E>\n---\nimport ‸kogo.test.Test", "test-import", "package kogo.test is for test files, those named _test.kg"},
		{"package kogo.test\nfun assertTrue(actual: Boolean) {\n}\n---\nfun f() {\n    ‸assertTrue(true)\n}", "test-only", "assertTrue is a test file's, one named _test.kg"},
		{"fun f(tx: Tx) {\n    tx.‸refusal(sql\"\")\n}", "test-only", "tx.refusal is a test file's, one named _test.kg"},
		{"fun f(): Int = ‸g()\n---\n/*test*/fun g(): Int = 1", "not-visible", "g is declared in the test file f2_test.kg, which kogo emit leaves out"},
	})
}
