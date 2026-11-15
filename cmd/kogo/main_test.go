package main

import (
	"bytes"
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

// kogo check passes examples 01 to 09 and 20, with the two files that
// hold what 01 refers to, as one program against the real prelude.
func TestExamples(t *testing.T) {
	files := examples(t, "0[0-9]_*.kg", "20_*.kg")
	if len(files) != 12 {
		t.Fatalf("%d examples, want 12", len(files))
	}
	if code, out := kogo(t, files); code != 0 || out != "" {
		t.Errorf("exit %d, want 0\n%s", code, out)
	}
}

// The Go that kogo emit writes for examples 01 to 06 and 20 is built
// with testdata/driver.go and run: each result was worked out by hand
// from the Kogo.
func TestEmittedGoRuns(t *testing.T) {
	// Inside the module, which holds the rt and the prelude it imports.
	dir, err := os.MkdirTemp(".", "emitted")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if code, out := kogo(t, examples(t, "0[0-6]_*.kg", "20_*.kg"), dir); code != 0 || out != "" {
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
card: FlatCard(label=flat, cents=500)
fee: 500
label: flat
first free: 3
none free: null
slug: acme-3
today in Sydney: 2026-11-16
today in New York: 2026-11-15
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
	code, out := kogo(t, map[string]string{"a.kg": "package a\n\nfun f(n: Int): Either<String, Int> = n.right()\n"}, dir)
	want := `{"file":"a.kg","line":3,"column":1,"code":"not-yet-emitted","message":"a fun that can fail (spec.md 9) is not emitted yet: the next slice of the emitter writes Either and Raise"}
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
	var stdout, stderr bytes.Buffer
	if code := run([]string{"check"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), `"code":"usage"`) {
		t.Errorf("no directory: exit %d, stderr %s", code, &stderr)
	}
	if code := run([]string{"check", filepath.Join(t.TempDir(), "none")}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), `"code":"read"`) {
		t.Errorf("a missing directory: exit %d, stderr %s", code, &stderr)
	}
}
