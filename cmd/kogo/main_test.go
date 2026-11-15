package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// kogo runs kogo check over a directory holding the files given, by
// name and text.
func kogo(t *testing.T, files map[string]string) (code int, stdout string) {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out, fatal bytes.Buffer
	code = run([]string{"check", dir}, &out, &fatal)
	if fatal.Len() > 0 {
		t.Fatalf("stderr: %s", &fatal)
	}
	return code, strings.ReplaceAll(out.String(), dir+string(filepath.Separator), "")
}

// testDB names the database the tests check sql literals against.
func testDB(t *testing.T) {
	url := os.Getenv("KOGO_TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://kogo:kogo@127.0.0.1/kogo_test"
	}
	t.Setenv("DATABASE_URL", url)
}

// kogo check passes every example, with the two files that hold what
// 01 refers to, as one program against the real prelude and, through
// the SQL pass, the test database.
func TestExamples(t *testing.T) {
	testDB(t)
	files := map[string]string{}
	for _, pattern := range []string{"*.kg"} {
		names, _ := filepath.Glob("../../examples/" + pattern)
		for _, name := range names {
			src, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			files[filepath.Base(name)] = string(src)
		}
	}
	if len(files) != 22 {
		t.Fatalf("%d examples, want 22", len(files))
	}
	if code, out := kogo(t, files); code != 0 || out != "" {
		t.Errorf("exit %d, want 0\n%s", code, out)
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
