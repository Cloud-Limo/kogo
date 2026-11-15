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
	if out != nil {
		args = []string{"emit", dir, out[0]}
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
	if len(files) != 23 {
		t.Fatalf("%d examples, want 23", len(files))
	}
	if code, out := kogo(t, files); code != 0 || out != "" {
		t.Errorf("exit %d, want 0\n%s", code, out)
	}
}

// The Go that kogo emit writes for the examples is built with
// testdata/driver.go and run, 10 to 19, 21 and 22 against the test
// database, 22 served over HTTP: each result was worked out by hand
// from the Kogo and the rows the driver begins with.
func TestEmittedGoRuns(t *testing.T) {
	testDB(t)
	// Inside the module, which holds the rt and the prelude it imports.
	dir, err := os.MkdirTemp(".", "emitted")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if code, out := kogo(t, examples(t, "*.kg"), dir); code != 0 || out != "" {
		t.Fatalf("exit %d, want 0\n%s", code, out)
	}
	driver, err := os.ReadFile("testdata/driver.go")
	if err != nil {
		t.Fatal(err)
	}
	driver = bytes.ReplaceAll(driver, []byte("ROOT"), []byte("kogo/cmd/kogo/"+filepath.Base(dir)))
	if err := os.WriteFile(filepath.Join(dir, "main.go"), driver, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("go", "run", "./"+dir).CombinedOutput()
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

// kogo ts writes a file for each api list, the same bytes each time it
// runs, and nothing for a program with errors.
func TestTS(t *testing.T) {
	src := t.TempDir()
	prog := "package a\nimport kogo.rpc.Request\ndata class Env(val n: Int)\nenum class State { On }\napi ops { f }\napi more { f }\n" +
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
