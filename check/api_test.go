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
		rpc()+"api ops unchecked { me ungated, f ungated, g ungated, }\napi other unchecked { me ungated }\n"+op("Ask", "Job?")+
			"fun g(db: Db, env: Env, request: Request, input: Tree): Either<String, List<Job>> = either { raise(\"no\") }\n"+
			"data class Job(val id: Uuid, val n: Int, val big: Long, val x: Double, val ok: Boolean, val tags: List<String>, val state: State, val notes: String?)\n"+
			"enum class State { Open, Done }\ndata class Ask(val kind: Kind, val code: String?)\nenum class Kind { ForBusiness, Alone }\n"+
			"data class Tree(val label: String, val kids: List<Tree>, val up: Tree?)",
		// An operation of another package, imported.
		"package kogo.rpc\nexternal class Request\n---\npackage a\nimport b.me\napi ops unchecked { me ungated }\n---\npackage b\nimport kogo.rpc.Request\ndata class E(val n: Int)\nfun me(db: Db, env: E, request: Request, input: Unit): Either<String, Unit> = either { }",
	)
	time := "\n---\npackage kogo.time\nexternal class Instant"
	refused(t, [][3]string{
		{rpc() + "api ops unchecked { ‸nope ungated }", "unknown-name", "nothing named nope"},
		{rpc() + "val v: Int = 1\napi ops unchecked { ‸v ungated }", "operation", "v is not a top-level fun"},
		{rpc() + "api ops unchecked { me ungated, ‸me ungated }", "duplicate", "me is already in api ops"},
		{rpc() + "fun me(s: String): Int = 1\napi ops unchecked { ‸me ungated }", "operation", "2 funs called me are in scope"},
		{rpc() + "fun f(tx: Tx, env: Env, request: Request): Either<String, Unit> = either { }\napi ops unchecked { ‸f ungated }", "operation", "f takes 3 parameters: an ungated operation takes four"},
		{rpc() + "fun f(s: String, env: Env, request: Request, input: Unit): Either<String, Unit> = either { }\napi ops unchecked { ‸f ungated }", "operation", "takes a String first: an ungated operation takes a Tx, or a Db"},
		{rpc() + "fun f(tx: Tx, env: Env, request: Int, input: Unit): Either<String, Unit> = either { }\napi ops unchecked { ‸f ungated }", "operation", "takes a Int third: an ungated operation takes a Request"},
		{rpc() + "fun f(tx: Tx, env: Env, request: Request, input: Unit): Either<Int, Unit> = either { }\napi ops unchecked { ‸f ungated }", "operation", "returns Either<Int, Unit>: an ungated operation returns Either<String, A>"},
		{rpc() + "fun <T> f(tx: Tx, env: Env, request: Request, input: Unit): Either<String, Unit> = either { }\napi ops unchecked { ‸f ungated }", "operation", "has type parameters"},
		{rpc() + "fun f(tx: Tx, env: Env, request: Request, input: Int = 1): Either<String, Unit> = either { }\napi ops unchecked { ‸f ungated }", "operation", "gives input a default value"},
		{rpc() + "data class E(val n: Int)\nfun f(tx: Tx, env: E, request: Request, input: Unit): Either<String, Unit> = either { }\napi ops unchecked { me ungated, ‸f ungated }", "operation", "f takes the environment E, and me takes Env"},
		{rpc() + "fun f(tx: Tx, env: Env?, request: Request, input: Unit): Either<String, Unit> = either { }\napi ops unchecked { ‸f ungated }", "operation", "f takes a Env? second: an ungated operation takes the list's environment there, a data class"},
		{rpc() + "fun f(tx: Tx, env: (Int) -> Int, request: Request, input: Unit): Either<String, Unit> = either { }\napi ops unchecked { ‸f ungated }", "operation", "takes a (Int) -> Int second"},
		{rpc() + "fun f(tx: Tx, env: Env, request: Request, input: Unit) = Unit\napi ops unchecked { ‸f ungated }", "operation", "f does not state its return type: an ungated operation returns Either<String, A>"},
		{rpc() + "api ops unchecked { ‸tx ungated }", "operation", "tx is not a top-level fun"},
		{rpc() + "‸api ops unchecked { }", "operation", "api ops names no fun"},
		{rpc() + "api ops unchecked { me ungated }\n‸api ops unchecked { me ungated }", "duplicate", "api ops is already declared at f2.kg:5: kogo ts writes each list to the file of its name, ops.ts"},
		{rpc() + "api ops unchecked { me ungated }\n---\npackage b\nimport t.me\n‸api ops unchecked { me ungated }", "duplicate", "api ops is already declared at f2.kg:5"},
		{rpc() + "api ops unchecked { me ungated }\n‸api Ops unchecked { me ungated }", "duplicate", "api Ops is declared at f2.kg:5 as api ops: kogo ts writes each list to the file of its name, and Ops.ts and ops.ts are one file where case is ignored"},
		{rpc() + "‸api outcome unchecked { me ungated }", "wire-name", "api outcome's interface in TypeScript would be Outcome, the answer's own type: rename the list"},
		{rpc("import kogo.time.Instant\n") + "api ops unchecked { ‸f ungated }\ndata class In(val n: Int, val at: Instant)\n" + op("In", "Unit") + time,
			"wire", "f's input holds In.at: Instant, which does not cross the wire: send text the server has formatted"},
		{rpc() + "api ops unchecked { ‸f ungated }\n" + op("Unit", "List<Map<String, Int>>"), "wire", "f's answer holds a Map<String, Int>, which does not cross"},
		{rpc() + "api ops unchecked { ‸f ungated }\n" + op("Unit", "Char"), "wire", "f's answer is a Char, which does not cross"},
		{rpc() + "api ops unchecked { ‸f ungated }\ndata class In(val s: S)\nsealed interface S {\n    data class A(val n: Int) : S\n}\n" + op("In", "Unit"),
			"wire", "f's input holds In.s: S, and no sealed interface or variant of one crosses the wire: send a data class, with an enum that says which case it is"},
		{rpc() + "api ops unchecked { ‸f ungated }\nsealed interface S {\n    data class A(val n: Int) : S\n}\n" + op("Unit", "List<S.A>"), "wire", "f's answer holds a S.A, and no sealed interface or variant of one crosses"},
		{rpc() + "api ops unchecked { ‸f ungated }\nsealed interface S {\n    data object O : S\n}\n" + op("Unit", "S.O?"), "wire", "f's answer is a S.O?, and no sealed interface or variant of one crosses"},
		{rpc() + "api ops unchecked { ‸f ungated }\ndata object Done\n" + op("Unit", "Done"), "wire", "f's answer is a Done, which does not cross the wire: send an enum, or Unit as the whole input or answer"},
		{rpc() + "api ops unchecked { ‸f ungated }\ndata class In(val u: Unit)\n" + op("In", "Unit"), "wire", "f's input holds In.u: Unit"},
		{rpc() + "api ops unchecked { ‸f ungated }\ndata class In(val g: () -> Int)\n" + op("In", "Unit"), "wire", "holds In.g: () -> Int, which does not cross"},
		{rpc() + "api ops unchecked { ‸f ungated }\ndata class Page<T>(val items: List<T>)\n" + op("Unit", "Page<Int>"), "wire", "and a type on the wire has no type parameters"},
		{rpc("import b.Box\n") + "api ops unchecked { ‸f ungated }\ndata class Out(val x: Item, val y: Box)\ndata class Item(val n: Int)\n" + op("Unit", "Out") +
			"\n---\npackage b\ndata class Box(val i: Item)\nenum class Item { One }",
			"wire-name", "f's answer reaches b.Item, and api ops reaches t.Item: two wire types of one name in a list are one type in TypeScript"},
		{rpc() + "api ops unchecked { ‸f ungated }\ndata class Outcome(val n: Int)\n" + op("Unit", "Outcome"), "wire-name", "Outcome is the answer's own type in TypeScript"},
		{rpc() + "api ops unchecked { ‸f ungated }\ndata class In(val ops: Ops)\nenum class Ops { One }\n" + op("In", "Unit"), "wire-name", "f's input reaches Ops, and Ops is api ops's interface in TypeScript: rename it"},
		{rpc() + "api ops unchecked { ‸f ungated }\ndata class string(val n: Int)\n" + op("string", "Unit"), "wire-name", "f's input reaches string, and a wire type's name begins with an upper-case letter"},
	})
}

// An operation whose result is not written gets the error of its
// body, not one that says it returns Unit.
func TestOperationResultUnwritten(t *testing.T) {
	_, errs := run(t, rpc()+"api ops unchecked { f ungated }\nfun f(tx: Tx, env: Env, request: Request, input: Unit) = either<String, Unit> { }")
	if len(errs) != 2 || errs[0].Code != "operation" || !strings.Contains(errs[0].Message, "f does not state its return type") || errs[1].Code != "return-type" {
		t.Errorf("got %v", errs)
	}
}

// The facts of a list: its operations in order, their kinds, and the
// wire types they reach.
func TestOperationFacts(t *testing.T) {
	info, errs := run(t, rpc()+"api ops unchecked { g ungated, f ungated, me ungated }\n"+op("Ask", "List<Job>?")+
		"fun g(db: Db, env: Env, request: Request, input: Job): Either<String, Unit> = either { }\n"+"data class Job(val state: State, val next: Job?)\n"+
		"enum class State { Open }\ndata class Ask(val kind: Kind, val code: String?)\nenum class Kind { ByCode, Alone }")
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
	want := "env Env; g tx=false Job -> Unit; f tx=true Ask -> List<Job>?; me tx=true Unit -> Unit; data class Job; enum class State; data class Ask; enum class Kind"
	if strings.Join(got, "; ") != want {
		t.Errorf("got  %s\nwant %s", strings.Join(got, "; "), want)
	}
}

// An ungated operation may answer a Reply: its answer is the wire's,
// and the Reply and its cookies are not. Nothing else crosses as one.
func TestReply(t *testing.T) {
	head := "package kogo.rpc\nexternal class Request\ndata class Cookie(val name: String)\ndata class Reply<A>(val answer: A, val cookies: List<Cookie>)\n---\n" +
		"import kogo.rpc.Request\nimport kogo.rpc.Reply\ndata class Env(val n: Int)\ndata class Job(val n: Int)\ndata class P(val tx: Tx)\n" +
		"fun g(tx: Tx, env: Env, request: Request): Either<String, P> = either { raise(\"no\") }\n"
	info, errs := run(t, head+"api ops unchecked { f ungated }\n"+op("Unit", "Reply<Job?>"))
	if errs != nil {
		t.Fatal(errs)
	}
	for _, a := range info.APIs {
		if got := fmt.Sprint(a.Ops[0].Answer, " ", len(a.Types), " ", a.Types[0].Name); got != "Job? 1 Job" {
			t.Errorf("got %s", got)
		}
	}
	why := "only an ungated operation answers with a Reply, its whole answer"
	refused(t, [][3]string{
		{head + "api ops unchecked { ‸f ungated }\n" + op("Reply<Job>", "Unit"), "wire", "f's input is a Reply<Job>, and " + why},
		{head + "api ops unchecked { ‸f ungated }\n" + op("Unit", "List<Reply<Job>>"), "wire", "f's answer holds a Reply<Job>, and " + why},
		{head + "api ops unchecked { ‸w gatedBy g }\nfun w(env: Env, p: P, input: Unit): Either<String, Reply<Unit>> = either { raise(\"no\") }", "wire", "w's answer is a Reply<Unit>, and " + why},
	})
}

// Sections 15.1 and 15.2: a list's check, a row's gate and its step,
// and that what one gives is what the next takes.
func TestGates(t *testing.T) {
	head := rpc() + `data class P(val n: Int)
data class Q(val n: Int)
data class In(val n: Int)
data class E2(val n: Int)
fun ours(tx: Tx, env: Env, request: Request): Either<String, Unit> = either { }
fun g(tx: Tx, env: Env, request: Request): Either<String, P> = either { P(1) }
fun own(tx: Tx, env: Env, request: Request, input: In): Either<String, P> = either { P(input.n) }
fun w(env: Env, p: P, input: In): Either<String, Int> = either { p.n }
fun v(env: Env, p: P, input: Unit): Either<String, Unit> = either { }
fun s(env: Env, p: P, input: In): Either<String, Unit> = either { }
`
	info, errs := run(t, head+"api ops checkedBy ours {\n    w gatedBy own after s\n    v gatedBy g\n    me ungated\n}\napi more unchecked { w gatedBy g, v gatedBy g }")
	if errs != nil {
		t.Fatal(errs)
	}
	for d, a := range info.APIs {
		got := fmt.Sprint(a.Check != nil, a.Env)
		for _, op := range a.Ops {
			got += fmt.Sprintf(" %s %v %v %v %v %v", op.Func.Name, op.Gate != nil, op.Proof, op.After != nil, op.Input, op.Tx)
		}
		want := map[string]string{
			"ops":  "true Env w true P true In false v true P false Unit false me false <nil> false Unit true",
			"more": "false Env w true P false In false v true P false Unit false",
		}[d.Name]
		if got != want {
			t.Errorf("api %s:\n got %s\nwant %s", d.Name, got, want)
		}
	}
	fun := func(decl string) string { return head + "fun " + decl + " = either { raise(\"no\") }\n" }
	refused(t, [][3]string{
		// The head.
		{head + "‸api ops { me ungated }", "no-check", "api ops names no check: write api ops checkedBy <check>, with the fun the server runs before every row, or api ops unchecked"},
		{head + "api ops checkedBy ‸nope { me ungated }", "unknown-name", "nothing named nope"},
		{fun("c(db: Db, env: Env, request: Request): Either<String, Unit>") + "api ops checkedBy ‸c { me ungated }", "check-shape", "c takes a Db first: a list's check takes a Tx"},
		{fun("c(tx: Tx, env: Env, request: Request, input: Unit): Either<String, Unit>") + "api ops checkedBy ‸c { me ungated }", "check-shape", "c takes 4 parameters: a list's check takes three, as fun c(tx: Tx, env: Env, request: Request)"},
		{fun("c(tx: Tx, env: Env, request: Request): Either<String, Int>") + "api ops checkedBy ‸c { me ungated }", "check-shape", "c returns Either<String, Int>: a list's check returns Either<String, Unit>"},
		{fun("c(tx: Tx, env: E2, request: Request): Either<String, Unit>") + "api ops checkedBy c { ‸me ungated }", "operation", "me takes the environment Env, and c takes E2"},
		// A gate.
		{head + "api ops unchecked { w gatedBy ‸nope }", "unknown-name", "nothing named nope"},
		{head + "fun x(tx: Tx, env: Env, request: Request): P = P(1)\napi ops unchecked { v gatedBy ‸x }", "gate", "x returns P: a gate returns Either<String, Proof>"},
		{fun("x(db: Db, env: Env, request: Request): Either<String, P>") + "api ops unchecked { v gatedBy ‸x }", "gate", "x takes a Db first: a gate takes a Tx"},
		{fun("x(tx: Tx, env: Env): Either<String, P>") + "api ops unchecked { v gatedBy ‸x }", "gate", "x takes 2 parameters: a gate takes three, as fun x(tx: Tx, env: Env, request: Request), and its operation's input fourth where it reads it"},
		{fun("x(tx: Tx, env: E2, request: Request): Either<String, P>") + "api ops unchecked { me ungated, v gatedBy ‸x }", "gate", "x takes the environment E2, and me takes Env"},
		{fun("x(tx: Tx, env: Env, request: Request, input: Unit): Either<String, P>") + "api ops unchecked { w gatedBy ‸x }", "gate", "x reads a Unit, and w's input is a In: a gate that reads an input takes its operation's"},
		{fun("w2(env: Env, p: P, input: In): Either<String, Unit>") + "api ops unchecked { w gatedBy own, w2 gatedBy ‸own }", "gate", "own is w's gate too, and it reads an input: a gate that more than one row names reads none, so write w2 a gate of its own or take the input out of own"},
		{fun("x(tx: Tx, env: Env, request: Request): Either<String, Tx>") + "fun y(env: Env, tx: Tx, input: Unit): Either<String, Unit> = either { }\n" + "api ops unchecked { y gatedBy ‸x }", "gate", "x gives a Tx: a gate gives a proof, not the handle, so give a data class that holds the Tx and what the gate read"},
		{fun("x(tx: Tx, env: Env, request: Request): Either<String, Db>") + "fun y(env: Env, db: Db, input: Unit): Either<String, Unit> = either { }\n" + "api ops unchecked { y gatedBy ‸x }", "gate", "x gives a Db: a gate gives a proof, not the handle"},
		{fun("x(tx: Tx, env: Env, request: Request): Either<String, Request>") + "fun y(env: Env, r: Request, input: Unit): Either<String, Unit> = either { }\n" + "api ops unchecked { y gatedBy ‸x }", "gate", "x gives a Request: a gate gives a proof, not the handle"},
		// Gated work.
		{head + "api ops unchecked { ‸me gatedBy g }", "operation", "me takes 4 parameters: gated work takes three, as fun me(env: Env, proof: Proof, input: Input)"},
		{fun("x(tx: Tx, p: P, input: Unit): Either<String, Unit>") + "api ops unchecked { ‸x gatedBy g }", "operation", "x takes a Tx first: gated work takes the list's environment there"},
		{fun("x(env: Env, q: Q, input: Unit): Either<String, Unit>") + "api ops unchecked { ‸x gatedBy g }", "operation", "x takes a Q second, and its gate g gives a P: gated work takes its gate's proof, so take a P or name the gate that gives a Q"},
		// Ungated work takes no proof.
		{head + "api ops unchecked { ‸v ungated }", "operation", "v takes 3 parameters: an ungated operation takes four, as fun v(tx: Tx, env: Env, request: Request, input: Input), with a Db for the Tx to open its own transactions and Unit for no input; work that takes a gate's proof names the gate in its row, with gatedBy"},
		// A step.
		{head + "api ops unchecked { w gatedBy g after ‸nope }", "unknown-name", "nothing named nope"},
		{head + "api ops unchecked { w gatedBy g after ‸me }", "step", "me takes 4 parameters: a step named with after takes three, as fun me(env: Env, proof: Proof, input: Input), with its gate's proof and its operation's input"},
		{fun("x(env: Env, q: Q, input: In): Either<String, Unit>") + "api ops unchecked { w gatedBy g after ‸x }", "step", "x takes a Q second, and w's gate g gives a P: a step takes its row's gate's proof"},
		{head + "api ops unchecked { v gatedBy g after ‸s }", "step", "s takes a In third, and v's input is a Unit: a step takes its operation's input"},
		{head + "api ops unchecked { w gatedBy g after ‸w }", "step", "w returns Either<String, Int>: a step named with after returns Either<String, Unit>"},
		{fun("x(env: E2, p: P, input: In): Either<String, Unit>") + "api ops unchecked { w gatedBy g after ‸x }", "step", "x takes the environment E2, and g takes Env"},
	})
}
