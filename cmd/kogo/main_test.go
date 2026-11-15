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

// kogo check passes examples 01 to 06 and 20, with the two files that
// hold what 01 refers to, as one program against the real prelude.
func TestExamples(t *testing.T) {
	files := map[string]string{}
	for _, pattern := range []string{"0[0-6]_*.kg", "20_*.kg"} {
		names, _ := filepath.Glob("../../examples/" + pattern)
		for _, name := range names {
			src, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			files[filepath.Base(name)] = string(src)
		}
	}
	if len(files) != 9 {
		t.Fatalf("%d examples, want 9", len(files))
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
	var stdout, stderr bytes.Buffer
	if code := run([]string{"check"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), `"code":"usage"`) {
		t.Errorf("no directory: exit %d, stderr %s", code, &stderr)
	}
	if code := run([]string{"check", filepath.Join(t.TempDir(), "none")}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), `"code":"read"`) {
		t.Errorf("a missing directory: exit %d, stderr %s", code, &stderr)
	}
}
