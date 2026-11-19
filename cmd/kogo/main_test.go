package main

import (
	"bytes"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// kogo runs kogo check over a directory holding the files given, by
// name and text, or with an out directory kogo emit.
func kogo(t *testing.T, files map[string]string, out ...string) (code int, stdout string) {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"check", dir}
	if len(out) == 1 {
		args = []string{"emit", dir, out[0]}
	} else if len(out) == 2 { // out and the test main
		args = []string{"test", dir, out[0], out[1]}
	}
	var stdoutText, fatal bytes.Buffer
	code = run(args, &stdoutText, &fatal)
	if fatal.Len() > 0 {
		t.Fatalf("stderr: %s", &fatal)
	}
	return code, strings.ReplaceAll(stdoutText.String(), dir+string(filepath.Separator), "")
}

// testDB names the database the tests check sql literals against.
func testDB(t *testing.T) {
	url := os.Getenv("KOGO_TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://kogo:kogo@127.0.0.1/kogo_test"
	}
	t.Setenv("DATABASE_URL", url)
}

// examples gives the examples whose names match the patterns.
func examples(t *testing.T, patterns ...string) map[string]string {
	files := map[string]string{}
	for _, pattern := range patterns {
		names, _ := filepath.Glob("../../examples/" + pattern)
		for _, name := range names {
			src, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			files[filepath.Base(name)] = string(src)
		}
	}
	return files
}

// kogo check passes every example, with the two files that hold what
// 01 refers to, as one program against the real prelude and, through
// the SQL pass, the test database.
func TestExamples(t *testing.T) {
	testDB(t)
	files := examples(t, "*.kg")
	if len(files) != 31 {
		t.Fatalf("%d examples, want 31", len(files))
	}
	if code, out := kogo(t, files); code != 0 || out != "" {
		t.Errorf("exit %d, want 0\n%s", code, out)
	}
}

// The Go that kogo emit writes for the examples is built with
// testdata/driver.go and run, 10 to 19, 21 and 22 against the test
// database, 22, 24, 25, 27 and 28 served over HTTP, 29 calling a local
// server, 30 keeping a message's bytes: each result was worked out by hand
// from the Kogo and the rows the driver begins with.
func TestEmittedGoRuns(t *testing.T) {
	testDB(t)
	// Inside the module, which holds the rt and the prelude it imports.
	dir, err := os.MkdirTemp(".", "emitted")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	files := examples(t, "*.kg")
	if code, out := kogo(t, files, dir); code != 0 || out != "" {
		t.Fatalf("exit %d, want 0\n%s", code, out)
	}
	driver, err := os.ReadFile("testdata/driver.go")
	if err != nil {
		t.Fatal(err)
	}
	driver = bytes.ReplaceAll(driver, []byte("ROOT"), []byte("github.com/Cloud-Limo/kogo/cmd/kogo/"+filepath.Base(dir)))
	if err := os.WriteFile(filepath.Join(dir, "main.go"), driver, 0o644); err != nil {
		t.Fatal(err)
	}
	// Built apart from the run: what go says about the build, such as
	// the modules it downloads, is not the program's output.
	prog := filepath.Join(dir, "prog")
	if out, err := exec.Command("go", "build", "-o", prog, "./"+dir).CombinedOutput(); err != nil {
		t.Fatalf("the emitted Go does not build: %v\n%s", err, out)
	}
	out, err := exec.Command(prog).CombinedOutput()
	want := `worth by card: 10237
worth: 10500
worth with no price: null
share: 8189
pay of a share: 8189
pay when fixed: 5000
pay of nothing: null
booking's pay: 8189
booking's pay with no card: 8400
booking's pay with no share: null
discounted: 9750
stack: DiscountStack(discounts=[Discount(description=Card, amount=-375, struck=false)], total=14625)
stack of nothing off: DiscountStack(discounts=[], total=10000)
abn: true
abn off by one: false
abn of letters: false
formatted: 51 824 753 556
too short to format: 123
ok: ok 7
refused: refused: late
issued can pay: true
paid can pay: false
nobody: nobody
ann: Ann on 0400 111 222
blank mobile: Bob
no mobile: Cy
mobile: 0400
mobile of none panics: person Cy has no mobile
signup: Welcome, ann@example.com.
signup with a bad email: ann is not an email address.
signup too young: You must be 18.
signup with both: ann is not an email address.
checked email: Either.Right(a@b)
load: Either.Right(one)
load of a missing item: Either.Left(Missing)
title of a missing item: Either.Left(No item 2.)
title with missing items after: Either.Left(No items after 1.)
card: FlatCard(label=flat, cents=500)
fee: 500
label: flat
first free: 3
none free: null
slug: acme-3
today in Sydney: 2026-11-16
today in New York: 2026-11-15
suppliers: [SupplierRow(id=00000000-0000-0000-0000-000000000002, legalName=Bolt Couriers, driverSharePercent=40)]
abn of acme: 51824753556
abn of bolt: null
abn of nobody: null
legal name: Bolt Couriers
ann administers acme: true
nobody administers acme: false
business: Business(id=00000000-0000-0000-0000-000000000001, slug=acme, legalName=Acme Pty Ltd, createdAt=2026-01-02T03:04:05Z, abn=51824753556, abnCheckedAt=null, payid=null, payidName=null)
business of no slug: null
administered: [Administered(id=00000000-0000-0000-0000-000000000001, legalName=Acme Pty Ltd, storefront=Storefront(id=00000000-0000-0000-0000-000000000004, slug=acme-store)), Administered(id=00000000-0000-0000-0000-000000000002, legalName=Bolt Couriers, storefront=null)]
drivers: [LinkedDriver(name=Ann, email=ann@example.com, active=true)]
inactive drivers: []
invoice: Invoice(id=00000000-0000-0000-0000-000000000005, number=INV-1, totalCents=1000)
invoice for update: Invoice(id=00000000-0000-0000-0000-000000000006, number=INV-2, totalCents=null)
no invoice: null
paid invoices: 1
business by id: Business(id=00000000-0000-0000-0000-000000000001, legalName=Acme Pty Ltd)
held: Acme Pty Ltd
hold of nobody: null
locked: 1
legal name of nobody panics: querySingle read no row, and reads exactly one: select legal_name from business where id = $1
fees of a missing invoice panics: The eligible fees changed before they were recorded
fees, the first rolled back: kotlin.Unit
invite's kind: outside
invite's share: null
accept with no share: Either.Left(NoShare)
invite again: 25
the same invite: true
accept: Either.Right(kotlin.Unit)
accept again: Either.Left(NoInvite)
suppliers after: [SupplierRow(id=00000000-0000-0000-0000-000000000002, legalName=Bolt Couriers, driverSharePercent=25)]
invoices: 200 {"type":"ok","value":[{"number":"INV-1","status":"paid","totalCents":1000},{"number":"INV-2","status":"sent","totalCents":null}]}
paid with no total: 200 {"type":"refused","message":"Invoice INV-2 has no total, so it cannot be paid."}
invoices after the refusal: 200 {"type":"ok","value":[{"number":"INV-1","status":"paid","totalCents":1000},{"number":"INV-2","status":"sent","totalCents":null}]}
back to a draft: 200 {"type":"ok","value":{"number":"INV-2","status":"draft","totalCents":null}}
no status: 400 setStatus: input.status is missing
not a status: 400 setStatus: input.status is not one of draft, sent, paid
no such operation: 404
look up: 200 {"type":"ok","value":{"number":"INV-1","status":"paid","totalCents":1000}}
look up again: 200 {"type":"ok","value":{"number":"INV-1","status":"paid","totalCents":1000}}
look up a missing invoice: 200 {"type":"refused","message":"There is no invoice INV-9."}
look up another missing invoice: 200 {"type":"refused","message":"There is no invoice INV-8."}
look up after two misses: 200 {"type":"refused","message":"Too many looks. Try again later."}
refused by the check: 403 Open the desk's page first.
the check before the name: 403 Open the desk's page first.
refused by a shared gate: 200 {"type":"refused","message":"This address may not use the desk."}
ungated, after the check: 200 {"type":"ok","value":2}
unpaid: 200 {"type":"ok","value":["INV-1","INV-2"]}
refused by its own gate: 200 {"type":"refused","message":"Invoice INV-2 cannot be changed."}
refused by the work: 200 {"type":"refused","message":"There is no invoice INV-9."}
refused by the step: 200 {"type":"refused","message":"No printer is set up."}
refused by work that is an action: 200 {"type":"refused","message":"No printer is set up."}
with 0 transactions open, front: invoice INV-1
sent after the step: 200 {"type":"ok","value":{"number":"INV-1","printer":"front"}}
mark paid: 200 {"type":"ok","value":1000}
mark paid again: 200 {"type":"refused","message":"Invoice INV-1 cannot be changed."}
status: 200 {"type":"ok","value":"paid"}
with 0 transactions open, front: a test page for 192.0.2.1
work that is an action: 200 {"type":"ok","value":"front"}
sign in: 200 ["session=Ann; Path=/; Max-Age=3600; HttpOnly; Secure; SameSite=Lax"] {"type":"ok","value":{"text":"Hello, Ann."}}
sign in refused: 200 [] {"type":"refused","message":"Say who you are."}
sign out: 200 ["session=; Path=/; Max-Age=0; HttpOnly; Secure; SameSite=Lax"] {"type":"ok","value":{}}
sign out refused: 200 [] {"type":"refused","message":"You are not signed in."}
no browser: 200 {"type":"ok","value":"nobody"}
a shop's page: 200 {"type":"ok","value":"nobody"}
the admin's page, typed in: 200 {"type":"ok","value":"nobody"}
an extra origin, with a slash: 200 {"type":"ok","value":"nobody"}
an old browser on a shop's page: 200 {"type":"ok","value":"nobody"}
another of the site's hosts: 403 Invalid origin
another site: 403 Invalid origin
same-origin, from an origin not ours: 403 Invalid origin
an origin not ours: 403 Invalid origin
a prefix of ours: 403 Invalid origin
null: 403 Invalid origin
null, same-origin: 403 Invalid origin
null, none: 403 Invalid origin
a session with its token: 200 {"type":"ok","value":"session 00000000-0000-0000-0000-00000000000a"}
a session with no token: 403 Invalid CSRF token
a session with another's token: 403 Invalid CSRF token
no browser, with a session and no token: 403 Invalid CSRF token
the Origin before the token: 403 Invalid origin
an expired session with no token: 403 Invalid CSRF token
an expired session with its token: 200 {"type":"ok","value":"nobody"}
a cookie that is no UUID: 200 {"type":"ok","value":"nobody"}
a cookie of no session: 200 {"type":"ok","value":"nobody"}
sign out: 200 {"type":"ok","value":{}}
an ended session with no token: 200 {"type":"ok","value":"nobody"}
the depot is sent: GET /v1/stock?item=bolt [Bearer of main ] 
stock: Either.Right(Stock(item=bolt, count=9007199254740993))
the depot is sent: POST /v1/items/nut%2F5%3F/reservations? [Bearer of main acme] {"count":2,"for":{"name":"Ann","gift":true},"notes":["fragile","this way up"]}
reserved: Either.Right(r-1)
the depot is sent: POST /v1/items/anvil/reservations? [Bearer of main acme] {"count":1,"for":{"name":"Ann","gift":true},"notes":["fragile","this way up"]}
sold out: Either.Left(refused 409: sold out)
the depot is sent: POST /v1/items/ghost/reservations? [Bearer of main acme] {"count":1,"for":{"name":"Ann","gift":true},"notes":["fragile","this way up"]}
no id: Either.Left(unreadable: the answer has no reservation)
nowhere: Either.Left(true)
queued: 1
read back as eml wrote it: true
  Message-ID: <receipt-7@shop.example>
  Date: Mon, 16 Nov 2026 14:30:00 +0000
  From: "Shop" <shop@shop.example>
  To: "Ann" <ann@a.example>
  Subject: Your receipt for 7
  MIME-Version: 1.0
  Content-Type: multipart/mixed; boundary="=_kogo-mixed"
  Content-Type: multipart/alternative; boundary="=_kogo-alternative"
  Content-Type: text/plain; charset=UTF-8
  Content-Type: text/html; charset=UTF-8
  Content-Type: text/plain
  Content-Disposition: attachment; filename="receipt-7.txt"
no letter: null
`
	if err != nil || string(out) != want {
		t.Errorf("%v\n got:\n%s\nwant:\n%s", err, out, want)
	}
}

// What the emitter does not write yet is an error like any other, and
// then nothing is written; with no go.mod above the out directory,
// kogo emit cannot say how the Go's packages import each other.
func TestEmitRefuses(t *testing.T) {
	dir, err := os.MkdirTemp(".", "emitted")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	code, out := kogo(t, map[string]string{"a.kg": "package a\n\nfun f(x: Int?): Int? = x.takeIf { true }\n"}, dir)
	want := `{"file":"a.kg","line":3,"column":24,"code":"not-yet-emitted","message":"a nullable type argument for T, which is used as T? where it is declared, is not emitted yet: pass a type that is not nullable, and keep the null outside"}
`
	if written, _ := os.ReadDir(dir); code != 1 || out != want || len(written) != 0 {
		t.Errorf("exit %d, want 1, with %d files written\n got %s\nwant %s", code, len(written), out, want)
	}
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "a.kg"), []byte("package a\n"), 0o644)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"emit", src, t.TempDir()}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), `"code":"no-module"`) {
		t.Errorf("no go.mod above the out directory: exit %d, stderr %s", code, &stderr)
	}
}

// A kogo.go that emit wrote for a package the program no longer has
// stops the next emit, which writes nothing and removes nothing, also
// when the out directory is reached by a link; the user's own files in
// it, a main.go, a go.mod and a kogo.go without the header, are left
// alone.
func TestEmitRefusesStale(t *testing.T) {
	dir := emitModule(t)
	// A kogo.go of the user's, without the header, is never refused.
	os.Mkdir(filepath.Join(dir, "lib"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, "lib", "kogo.go"), []byte("// Package lib is the user's, longer than kogo's header.\npackage lib\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	emit := func(pkg, out string) (int, string, string) {
		os.WriteFile(filepath.Join(src, "a.kg"), []byte("package "+pkg+"\n\nfun f(x: Int): Int = x\n"), 0o644)
		var stdout, stderr bytes.Buffer
		code := run([]string{"emit", src, out}, &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}
	if code, out, fatal := emit("app.old", dir); code != 0 || out != "" || fatal != "" {
		t.Fatalf("first emit: exit %d, want 0\n%s%s", code, out, fatal)
	}
	if code, out, fatal := emit("app.old", dir); code != 0 || out != "" || fatal != "" {
		t.Fatalf("emit again: exit %d, want 0\n%s%s", code, out, fatal)
	}
	// An out that is a link to the directory is the directory.
	link := filepath.Join(dir, "link")
	if err := os.Symlink(".", link); err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{dir, link} {
		stale := filepath.Join(out, "app", "old", "kogo.go")
		want := `{"file":"` + stale + `","line":0,"column":0,"code":"stale-output","message":"` + stale + ` was written by kogo emit for a package this program no longer has, and the Go under ` + out + ` would build it beside the new: remove it, or emit into another directory"}
`
		code, got, fatal := emit("app.new", out)
		if _, err := os.Stat(filepath.Join(dir, "app", "new")); code != 2 || got != "" || fatal != want || !os.IsNotExist(err) {
			t.Errorf("after a rename, into %s: exit %d, want 2, with app/new written: %v\n got %s%s\nwant %s", out, code, err == nil, got, fatal, want)
		}
	}
	// The refusal removed nothing.
	if err := os.Remove(filepath.Join(dir, "app", "old", "kogo.go")); err != nil {
		t.Fatal(err)
	}
	if code, out, fatal := emit("app.new", dir); code != 0 || out != "" || fatal != "" {
		t.Errorf("with the stale file removed: exit %d, want 0\n%s%s", code, out, fatal)
	}
}

// What under <out> cannot be read is the user's: kogo emit passes over
// it rather than stopping.
func TestEmitPassesUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file of mode 000")
	}
	dir := emitModule(t)
	data, hidden := filepath.Join(dir, "data"), filepath.Join(dir, "x", "kogo.go")
	os.Mkdir(filepath.Join(dir, "x"), 0o755)
	if err := os.WriteFile(hidden, []byte("// Code generated by kogo emit\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(data, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(data, 0o755)
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "a.kg"), []byte("package app\n\nfun f(x: Int): Int = x\n"), 0o644)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"emit", src, dir}, &stdout, &stderr); code != 0 || stdout.Len() > 0 || stderr.Len() > 0 {
		t.Errorf("exit %d, want 0\n%s%s", code, stdout.String(), stderr.String())
	}
}

// failing is a test file of three tests: an assertion that fails
// after a row is written, a statement Postgres refuses outside
// tx.refusal, which is a bug, and one that passes only if both were
// rolled back.
const failing = `package example.ops

import kogo.test.assertEquals
import kogo.test.assertNull
import kogo.test.test

tests failing<OpsEnv> { wrongSum, bug, timedOut, clean }

val wrongSum =
    test("adds one and one") { tx, env ->
        draft(tx, "F-1", null)
        assertEquals(3L, 1L + 1L, "the sum")
    }

val bug =
    test("writes one number twice") { tx, env ->
        draft(tx, "F-2", null)
        draft(tx, "F-2", null)
    }

val timedOut =
    test("is stopped, not refused") { tx, env ->
        tx.execute(sql"set local statement_timeout = 50")
        assertEquals("57014", tx.refusal(sql"select pg_sleep(1)")?.sqlState)
    }

val clean =
    test("sees no row of a test that failed") { tx, env ->
        assertNull(tx.queryOne<String>(sql"select number from invoice where number like 'F-%' limit 1"))
    }
`

// kogo test runs the tests of examples/23_test.kg, which pass: among
// them one that sees no row of the test before it, and one that a
// refused statement gives a Refusal and leaves its transaction open.
// A test that fails is printed with its sentence, where it is
// declared and the line that failed, and the tests after it run; and
// kogo emit leaves the test files out.
func TestKogoTest(t *testing.T) {
	testDB(t)
	dir, err := os.MkdirTemp(".", "emitted") // inside the module, as in TestEmittedGoRuns
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	main, err := os.ReadFile("testdata/testmain.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(main), "ROOT", "github.com/Cloud-Limo/kogo/cmd/kogo/"+filepath.Base(dir))
	test := func(files map[string]string) (int, string) {
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return kogo(t, files, dir, dir)
	}
	files := examples(t, "22_api.kg", "23_test.kg")
	if code, out := test(files); code != 0 || out != "" {
		t.Errorf("exit %d, want 0 and no output\n%s", code, out)
	}
	files["failing_test.kg"] = failing
	// A list whose environment the main gives none of, and one in a
	// package the main does not import.
	files["unit_test.kg"] = "package example.ops\n\nimport kogo.test.test\n\ntests units<Unit> { one }\n\nval one =\n    test(\"runs\") { tx, env -> }\n"
	files["lone_test.kg"] = "package example.lone\n\nimport kogo.test.test\n\ntests lone<Unit> { one }\n\nval one =\n    test(\"runs\") { tx, env -> }\n"
	code, out := test(files)
	want := `{"file":"failing_test.kg","line":9,"column":1,"code":"test-failed","test":"adds one and one","message":"the sum. Expected <3>, actual <2>.","at":"failing_test.kg:12"}
{"file":"failing_test.kg","line":15,"column":1,"code":"test-failed","test":"writes one number twice","message":"ERROR: duplicate key value violates unique constraint \"invoice_number_key\" (SQLSTATE 23505)","at":"23_test.kg:16"}
{"file":"failing_test.kg","line":21,"column":1,"code":"test-failed","test":"is stopped, not refused","message":"ERROR: canceling statement due to statement timeout (SQLSTATE 57014)","at":"failing_test.kg:24"}
{"file":"unit_test.kg","line":5,"column":1,"code":"no-env","test":"example.ops.units","message":"the test main gives test.Run no environment of the type that tests list example.ops.units takes: pass one","at":""}
{"file":"lone_test.kg","line":5,"column":1,"code":"not-linked","test":"example.lone.lone","message":"the test main does not import the package of tests list example.lone.lone, so its tests did not run: import it, as _ if need be","at":""}
`
	if code != 1 || out != want {
		t.Errorf("exit %d, want 1\n got %s\nwant %s", code, out, want)
	}
	delete(files, "lone_test.kg") // its package's Go is stale once emit leaves it out
	os.RemoveAll(filepath.Join(dir, "example/lone"))
	if code, out := kogo(t, files, dir); code != 0 || out != "" {
		t.Fatalf("exit %d, want 0\n%s", code, out)
	}
	if text, err := os.ReadFile(filepath.Join(dir, "example/ops/kogo.go")); err != nil || bytes.Contains(text, []byte("InvoiceTests")) {
		t.Errorf("kogo emit wrote the tests of a _test.kg file, or nothing: %v", err)
	}
}

// emitModule makes a directory to emit into, inside a Go module of its
// own that holds the user's main.go, and removes it when t ends.
func emitModule(t *testing.T) string {
	dir, err := os.MkdirTemp(".", "emitted")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	for name, text := range map[string]string{"main.go": "package main\n\nfunc main() {}\n", "go.mod": "module emitted\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// kogo ts writes a file for each api list, the same bytes each time it
// runs, and nothing for a program with errors.
func TestTS(t *testing.T) {
	src := t.TempDir()
	prog := "package a\nimport kogo.rpc.Request\ndata class Env(val n: Int)\nenum class State { On }\napi ops unchecked { f ungated }\napi more unchecked { f ungated }\n" +
		"fun f(tx: Tx, env: Env, request: Request, input: Unit): Either<String, State> = either { State.On }\n"
	ts := func(prog string) (code int, written map[string]string) {
		os.WriteFile(filepath.Join(src, "a.kg"), []byte(prog), 0o644)
		out := t.TempDir()
		var stdout, stderr bytes.Buffer
		code = run([]string{"ts", src, out}, &stdout, &stderr)
		names, _ := os.ReadDir(out)
		written = map[string]string{}
		for _, name := range names {
			text, _ := os.ReadFile(filepath.Join(out, name.Name()))
			written[name.Name()] = string(text)
		}
		return code, written
	}
	code, first := ts(prog)
	if code != 0 || len(first) != 2 || !strings.Contains(first["ops.ts"], `export type State = "On"`) || !strings.Contains(first["more.ts"], "export interface More {") {
		t.Fatalf("exit %d, wrote %v", code, first)
	}
	if _, again := ts(prog); !maps.Equal(first, again) {
		t.Errorf("wrote %v, then %v", first, again)
	}
	// A Reply's answer alone crosses: the page sees no Reply or Cookie.
	reply := strings.NewReplacer("import kogo.rpc.Request\n", "import kogo.rpc.Request\nimport kogo.rpc.Reply\n",
		"Either<String, State> = either { State.On }", "Either<String, Reply<State>> = either { Reply(State.On, listOf()) }").Replace(prog)
	if code, written := ts(reply); code != 0 || written["ops.ts"] != first["ops.ts"] {
		t.Errorf("a Reply: exit %d, wrote %v", code, written)
	}
	if code, written := ts(prog + "val v: Int = \"no\"\n"); code != 1 || len(written) != 0 {
		t.Errorf("a program with errors: exit %d, wrote %v", code, written)
	}
}

// An error is one JSON object on a line, with its file and position;
// a file that does not parse is reported, and then nothing is checked,
// since the others would be checked without its declarations.
func TestErrors(t *testing.T) {
	code, out := kogo(t, map[string]string{
		"a.kg": "package a\n\nfun f(xs: List<Int>): Long = xs.sumOf { it }\n",
		"b.kg": "package a\n\nfun g( = 1\n",
	})
	want := `{"file":"b.kg","line":3,"column":8,"code":"syntax","message":"expected a name, found \"=\""}
`
	if code != 1 || out != want {
		t.Errorf("exit %d, want 1\n got %s\nwant %s", code, out, want)
	}
	code, out = kogo(t, map[string]string{
		"a.kg": "package a\n\nfun f(xs: List<Int>): Long = xs.sumOf { it }\n",
	})
	want = `{"file":"a.kg","line":3,"column":30,"code":"type-mismatch","message":"this is a Int where a Long is wanted: write .toLong()"}
`
	if code != 1 || out != want {
		t.Errorf("exit %d, want 1\n got %s\nwant %s", code, out, want)
	}
	// A literal is checked against the database, and its fault
	// reported at its place.
	testDB(t)
	code, out = kogo(t, map[string]string{
		"a.kg": "package a\n\nfun f(tx: Tx): Int = tx.execute(sql\"\"\"\n    delete from business\n    where nam = 'x'\"\"\")\n",
	})
	want = `{"file":"a.kg","line":5,"column":11,"code":"sql-rejected","message":"column \"nam\" does not exist (Postgres 42703)"}
`
	if code != 1 || out != want {
		t.Errorf("exit %d, want 1\n got %s\nwant %s", code, out, want)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"check"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), `"code":"usage"`) {
		t.Errorf("no directory: exit %d, stderr %s", code, &stderr)
	}
	if code := run([]string{"check", filepath.Join(t.TempDir(), "none")}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), `"code":"read"`) {
		t.Errorf("a missing directory: exit %d, stderr %s", code, &stderr)
	}
}

// A <dir> that is a link to a program's directory is that directory:
// its errors are found and name each file under the link as given.
func TestLinkedDir(t *testing.T) {
	dir, link := t.TempDir(), filepath.Join(t.TempDir(), "link")
	os.WriteFile(filepath.Join(dir, "a.kg"), []byte("package a\n\nfun f(): Int = nope\n"), 0o644)
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"check", link}, &stdout, &stderr)
	if want := `{"file":"` + filepath.Join(link, "a.kg") + `","line":3,"column":16,"code":"unknown-name"`; code != 1 || !strings.HasPrefix(stdout.String(), want) {
		t.Errorf("exit %d, want 1\n got %s%s\nwant %s...", code, &stdout, &stderr, want)
	}
	// A link that names nothing is a <dir> that cannot be read.
	dangling := filepath.Join(t.TempDir(), "dangling")
	os.Symlink(filepath.Join(dir, "nowhere"), dangling)
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"check", dangling}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), `"code":"read"`) {
		t.Errorf("dangling: exit %d, want 2 and read\n got %s%s", code, &stdout, &stderr)
	}
}

// A <dir> with no .kg file under it is not a program: each command
// refuses it rather than pass it.
func TestNoSource(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("package a\n"), 0o644)
	for _, args := range [][]string{{"check", dir}, {"emit", dir, t.TempDir()}, {"ts", dir, t.TempDir()}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), `"code":"no-source"`) || !strings.Contains(stderr.String(), dir) {
			t.Errorf("%s: exit %d, want 2 and no-source naming %s\n got %s%s", args[0], code, dir, &stdout, &stderr)
		}
	}
}

// A program with no sql literal is checked without a database; one
// with a literal and no database cannot be.
func TestNoDatabase(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://nobody@127.0.0.1:1/none")
	if code, out := kogo(t, map[string]string{"a.kg": "package a\n\nfun f(): Int = 1\n"}); code != 0 || out != "" {
		t.Errorf("no literal: exit %d, want 0\n%s", code, out)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.kg"), []byte("package a\n\nfun f(tx: Tx): Int = tx.execute(sql\"select 1\")\n"), 0o644)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"check", dir}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), `"code":"sql-connect"`) {
		t.Errorf("a literal and no database: exit %d, stderr %s", code, &stderr)
	}
}
