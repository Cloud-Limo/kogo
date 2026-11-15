package check

import (
	"fmt"
	"strings"
	"testing"
)

// rpc gives the few lines of kogo.rpc that operations need, and the
// head of a program: its imports, an environment and an operation.
func rpc(imports ...string) string {
	return "package kogo.rpc\nexternal class Request\n---\nimport kogo.rpc.Request\n" + strings.Join(imports, "") +
		"data class Env(val n: Int)\nfun me(tx: Tx, env: Env, request: Request, input: Unit): Either<String, Unit> = either { }\n"
}

// op declares the operation f with input in and answer out.
func op(in, out string) string {
	return fmt.Sprintf("fun f(tx: Tx, env: Env, request: Request, input: %s): Either<String, %s> = either { raise(\"no\") }\n", in, out)
}

// Section 15: what an api list names, an operation's shape, and the
// wire.
func TestOperations(t *testing.T) {
	passes(t,
		rpc()+"api ops { me, f, g, }\napi other { me }\n"+op("Ask", "Job?")+
			"fun g(db: Db, env: Env, request: Request, input: Tree): Either<String, List<Job>> = either { raise(\"no\") }\n"+
			"data class Job(val id: Uuid, val n: Int, val big: Long, val x: Double, val ok: Boolean, val tags: List<String>, val state: State, val notes: String?)\n"+
			"enum class State { Open, Done }\nsealed interface Ask {\n    data class ForBusiness(val code: String) : Ask\n    data object Alone : Ask\n}\n"+
			"data class Tree(val label: String, val kids: List<Tree>, val up: Tree?)",
		// An operation of another package, imported.
		"package kogo.rpc\nexternal class Request\n---\npackage a\nimport b.me\napi ops { me }\n---\npackage b\nimport kogo.rpc.Request\ndata class E(val n: Int)\nfun me(db: Db, env: E, request: Request, input: Unit): Either<String, Unit> = either { }",
	)
	time := "\n---\npackage kogo.time\nexternal class Instant"
	refused(t, [][3]string{
		{rpc() + "api ops { ‸nope }", "unknown-name", "nothing named nope"},
		{rpc() + "val v: Int = 1\napi ops { ‸v }", "operation", "v is not a top-level fun"},
		{rpc() + "api ops { me, ‸me }", "duplicate", "me is already in api ops"},
		{rpc() + "fun me(s: String): Int = 1\napi ops { ‸me }", "operation", "2 funs called me are in scope"},
		{rpc() + "fun f(tx: Tx, env: Env, request: Request): Either<String, Unit> = either { }\napi ops { ‸f }", "operation", "f takes 3 parameters: an operation takes four"},
		{rpc() + "fun f(s: String, env: Env, request: Request, input: Unit): Either<String, Unit> = either { }\napi ops { ‸f }", "operation", "takes a String first: an operation takes a Tx, or a Db"},
		{rpc() + "fun f(tx: Tx, env: Env, request: Int, input: Unit): Either<String, Unit> = either { }\napi ops { ‸f }", "operation", "takes a Int third: an operation takes a Request"},
		{rpc() + "fun f(tx: Tx, env: Env, request: Request, input: Unit): Either<Int, Unit> = either { }\napi ops { ‸f }", "operation", "returns Either<Int, Unit>: an operation returns Either<String, A>"},
		{rpc() + "fun <T> f(tx: Tx, env: Env, request: Request, input: Unit): Either<String, Unit> = either { }\napi ops { ‸f }", "operation", "has type parameters"},
		{rpc() + "fun f(tx: Tx, env: Env, request: Request, input: Int = 1): Either<String, Unit> = either { }\napi ops { ‸f }", "operation", "gives input a default value"},
		{rpc() + "data class E(val n: Int)\nfun f(tx: Tx, env: E, request: Request, input: Unit): Either<String, Unit> = either { }\napi ops { me, ‸f }", "operation", "f takes the environment E, and me takes Env"},
		{rpc() + "fun f(tx: Tx, env: Env?, request: Request, input: Unit): Either<String, Unit> = either { }\napi ops { ‸f }", "operation", "f takes a Env? second: an operation's environment is a data class"},
		{rpc() + "fun f(tx: Tx, env: (Int) -> Int, request: Request, input: Unit): Either<String, Unit> = either { }\napi ops { ‸f }", "operation", "takes a (Int) -> Int second"},
		{rpc() + "fun f(tx: Tx, env: Env, request: Request, input: Unit) = Unit\napi ops { ‸f }", "operation", "f does not state its return type: an operation returns Either<String, A>"},
		{rpc() + "api ops { ‸tx }", "operation", "tx is not a top-level fun"},
		{rpc() + "‸api ops { }", "operation", "api ops names no fun"},
		{rpc() + "api ops { me }\n‸api ops { me }", "duplicate", "api ops is already declared at f2.kg:5: kogo ts writes each list to the file of its name, ops.ts"},
		{rpc() + "api ops { me }\n---\npackage b\nimport t.me\n‸api ops { me }", "duplicate", "api ops is already declared at f2.kg:5"},
		{rpc("import kogo.time.Instant\n") + "api ops { ‸f }\ndata class In(val n: Int, val at: Instant)\n" + op("In", "Unit") + time,
			"wire", "f's input holds In.at: Instant, which does not cross the wire: send text the server has formatted"},
		{rpc() + "api ops { ‸f }\n" + op("Unit", "List<Map<String, Int>>"), "wire", "f's answer holds a Map<String, Int>, which does not cross"},
		{rpc() + "api ops { ‸f }\n" + op("Unit", "Char"), "wire", "f's answer is a Char, which does not cross"},
		// A data object crosses as a variant only, wherever else it is reached.
		{rpc() + "api ops { me, g, ‸f }\nsealed interface S {\n    data object O : S\n}\n" + op("Unit", "S.O") +
			"fun g(tx: Tx, env: Env, request: Request, input: S): Either<String, Unit> = either { }", "wire", "f's answer is a S.O, which does not cross"},
		{rpc() + "api ops { ‸f }\ndata class In(val u: Unit)\n" + op("In", "Unit"), "wire", "f's input holds In.u: Unit"},
		{rpc() + "api ops { ‸f }\ndata class In(val g: () -> Int)\n" + op("In", "Unit"), "wire", "holds In.g: () -> Int, which does not cross"},
		{rpc() + "api ops { ‸f }\nsealed interface S<out T> {\n    data class A<T>(val v: T) : S<T>\n}\n" + op("S<Int>", "Unit"), "wire", "f's input is a S<Int>, and a type on the wire has no type parameters"},
		{rpc() + "api ops { ‸f }\ndata class Page<T>(val items: List<T>)\n" + op("Unit", "Page<Int>"), "wire", "and a type on the wire has no type parameters"},
		{rpc() + "api ops { ‸f }\nsealed interface S {\n    data class A(val type: String) : S\n}\n" + op("S", "Unit"), "wire", "f's input holds S.A.type, and a variant on the wire has no property type"},
		// A nested variant goes by its own name.
		{rpc() + "api ops { ‸f }\ndata class Out(val x: X, val y: Y)\nsealed interface X {\n    data object Item : X\n}\nsealed interface Y {\n    data class Item(val n: Int) : Y\n}\n" + op("Unit", "Out"),
			"wire-name", "f's answer reaches t.Y.Item, and api ops reaches t.X.Item: two wire types of one name in a list are one type in TypeScript"},
		{rpc() + "api ops { ‸f }\ndata class Outcome(val n: Int)\n" + op("Unit", "Outcome"), "wire-name", "Outcome is the answer's own type in TypeScript"},
	})
}

// An operation whose result is not written gets the error of its
// body, not one that says it returns Unit.
func TestOperationResultUnwritten(t *testing.T) {
	_, errs := run(t, rpc()+"api ops { f }\nfun f(tx: Tx, env: Env, request: Request, input: Unit) = either<String, Unit> { }")
	if len(errs) != 2 || errs[0].Code != "operation" || !strings.Contains(errs[0].Message, "f does not state its return type") || errs[1].Code != "return-type" {
		t.Errorf("got %v", errs)
	}
}

// The facts of a list: its operations in order, their kinds, and the
// wire types they reach, a sealed interface before its variants even
// where a variant is reached first.
func TestOperationFacts(t *testing.T) {
	info, errs := run(t, rpc()+"api ops { g, f, me }\n"+op("Ask", "List<Job>?")+
		"fun g(tx: Tx, env: Env, request: Request, input: Ask.ByCode): Either<String, Unit> = either { }\n"+"data class Job(val state: State, val next: Job?)\n"+
		"enum class State { Open }\nsealed interface Ask {\n    data class ByCode(val code: String) : Ask\n    data object Alone : Ask\n}")
	if errs != nil || len(info.APIs) != 1 {
		t.Fatalf("got %v and %d lists", errs, len(info.APIs))
	}
	var got []string
	for _, a := range info.APIs {
		got = append(got, "env "+a.Env.String())
		for _, o := range a.Ops {
			got = append(got, fmt.Sprintf("%s tx=%v %s -> %s", o.Func.Name, o.Tx, o.Input, o.Answer))
		}
		for _, d := range a.Types {
			got = append(got, d.Kind+" "+d.Name)
		}
	}
	want := "env Env; g tx=true Ask.ByCode -> Unit; f tx=true Ask -> List<Job>?; me tx=true Unit -> Unit; sealed interface Ask; data class Ask.ByCode; data object Ask.Alone; data class Job; enum class State"
	if strings.Join(got, "; ") != want {
		t.Errorf("got  %s\nwant %s", strings.Join(got, "; "), want)
	}
}
