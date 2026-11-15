package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// kogoSQL runs the command against the test database and gives its
// output, one line an element, and its exit code.
func kogoSQL(t *testing.T, args ...string) (stdout []byte, lines []string, code int) {
	url := os.Getenv("KOGO_TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://kogo:kogo@127.0.0.1/kogo_test"
	}
	t.Setenv("DATABASE_URL", url)
	var out, errs bytes.Buffer
	code = run(args, &out, &errs)
	if code == 2 {
		t.Fatalf("kogo-sql could not run (create the test database with `mise run testdb`): %s", errs.String())
	}
	return out.Bytes(), strings.Split(strings.TrimSpace(out.String()+errs.String()), "\n"), code
}

func TestCheckExamples(t *testing.T) {
	want := []string{
		"10_sql_list.kg:7:27 (operatorId: Uuid) -> id: Uuid, legal_name: String, driver_share_percent: Int",
		"11_sql_one_scalar.kg:6:26 (businessId: Uuid) -> abn: String?",
		"11_sql_one_scalar.kg:9:28 (businessId: Uuid) -> legal_name: String",
		"11_sql_one_scalar.kg:12:15 (personId: Uuid, businessId: Uuid) -> ?column?: Int?",
		"12_sql_table_row.kg:17:27 (slug: String) -> id: Uuid, slug: String, legal_name: String, created_at: Instant, " +
			"abn: String?, abn_checked_at: Instant?, payid: String?, payid_name: String?",
		"13_sql_upsert_returning.kg:17:35 (id: Uuid, businessId: Uuid, kind: InviteKind, personId: Uuid, invitedBy: Uuid, share: Int) -> " +
			"id: Uuid, business_id: Uuid, kind: InviteKind, person_id: Uuid, invited_by: Uuid, created_at: Instant, driver_share_percent: Int?",
		"14_sql_outer_join.kg:9:28 (personId: Uuid) -> id: Uuid, legal_name: String, storefront_id: Uuid?, storefront_slug: String?",
		"15_sql_optional_filter.kg:7:28 (active: Boolean) -> name: String, email: String, active: Boolean",
		"16_sql_update_any.kg:6:20 (at: Instant, reference: String, invoiceIds: List<Uuid>) -> ",
		"17_sql_two_statements.kg:8:30 (id: Uuid) -> id: Uuid, number: String, total_cents: Long?",
		"17_sql_two_statements.kg:10:30 (id: Uuid) -> id: Uuid, number: String, total_cents: Long?",
		"17_sql_two_statements.kg:14:26 () -> n: Long",
		"18_tx.kg:14:40 (inviteId: Uuid) -> business_id: Uuid, driver_share_percent: Int?",
		"18_tx.kg:18:20 (share: Int, invite.businessId: Uuid, supplierId: Uuid) -> ",
		"18_tx.kg:22:20 (inviteId: Uuid) -> ",
		"19_markers.kg:15:27 (id: Uuid) -> id: Uuid, legal_name: String",
	}
	_, lines, code := kogoSQL(t, "check", "../../examples")
	if code != 0 || len(lines) != len(want) {
		t.Fatalf("exit %d with %d lines, want 0 with %d:\n%s", code, len(lines), len(want), strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[8], `"List<Uuid>"`) {
		t.Errorf("a type is not written as Kogo writes it: %s", lines[8])
	}
	for i, line := range lines {
		var sig signature
		if err := json.Unmarshal([]byte(line), &sig); err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		var params, columns []string
		for _, p := range sig.Params {
			params = append(params, p.Name+": "+p.Type)
		}
		for _, c := range sig.Columns {
			columns = append(columns, c.Name+": "+c.Type)
		}
		got := fmt.Sprintf("%s:%d:%d (%s) -> %s", filepath.Base(sig.File), sig.Line, sig.Column,
			strings.Join(params, ", "), strings.Join(columns, ", "))
		if got != want[i] {
			t.Errorf("\n got %s\nwant %s", got, want[i])
		}
	}
}

func TestWrongStatements(t *testing.T) {
	want := []string{
		`5:36 sql-rejected column "legal_nam" does not exist`,
		`10:26 sql-rejected operator does not exist: text = integer`,
		`14:84 sql-rejected invalid input value for enum invoice_status: "payed"`,
		`20:73 sql-rejected operator does not exist: uuid = citext`,
		`24:65 sql-rejected operator does not exist: text < integer`,
	}
	_, lines, code := kogoSQL(t, "check", "testdata")
	unclosed := `{"file":"testdata/unclosed.kg","line":4,"column":40,"code":"sql-unterminated","message":"this sql literal has no closing quote"}`
	if lines[0] != unclosed {
		t.Errorf("\n got %s\nwant %s", lines[0], unclosed)
	}
	lines = lines[1:]
	if !strings.Contains(lines[4], "text < integer") {
		t.Errorf("a message is escaped: %s", lines[4])
	}
	if code != 1 || len(lines) != len(want) {
		t.Fatalf("exit %d with %d lines, want 1 with %d:\n%s", code, len(lines), len(want), strings.Join(lines, "\n"))
	}
	for i, line := range lines {
		var d diagnostic
		if err := json.Unmarshal([]byte(line), &d); err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		got := fmt.Sprintf("%d:%d %s %s", d.Line, d.Column, d.Code, d.Message)
		if d.File != "testdata/wrong.kg" || !strings.HasPrefix(got, want[i]) {
			t.Errorf("\n got %s %s\nwant testdata/wrong.kg %s", d.File, got, want[i])
		}
	}
}

// The Go emitted for examples/ is built with testdata/driver.go and run.
func TestEmittedGoRuns(t *testing.T) {
	src, lines, code := kogoSQL(t, "emit", "../../examples", "main")
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, strings.Join(lines, "\n"))
	}
	// Inside the module, so the program finds pgx as this module pins it.
	dir, err := os.MkdirTemp(".", "emitted")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	driver, err := os.ReadFile("testdata/driver.go")
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{"main.go": driver, "queries.go": src} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command("go", "run", "./"+dir).CombinedOutput()
	want := `supplier: Bolt Couriers 40
abn of bolt is null: true
administered: Acme Pty Ltd storefront: acme-store
administered: Bolt Couriers storefront: none
invite: outside share is null: true
invite again: outside 25
drivers, any: 1 inactive: 0
fees accounted: 2
paid invoices: 1
`
	if err != nil || string(out) != want {
		t.Errorf("%v\n got:\n%s\nwant:\n%s", err, out, want)
	}
}

func TestFuncNamesAreDistinct(t *testing.T) {
	used := map[string]bool{}
	seen := map[string]bool{}
	for _, file := range []string{"a-b", "a_b", "sub/x", "sub_x"} {
		name := funcName(file, 3, 10, used)
		if seen[name] {
			t.Errorf("%s is named %s, as another file's literal is", file, name)
		}
		seen[name] = true
	}
}
