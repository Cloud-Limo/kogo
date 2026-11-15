package sqlpass

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestEmitFieldNamesAreDistinct(t *testing.T) {
	text := Type{Name: "String"}
	st := &Statement{SQL: "select", Columns: []Column{
		{Name: "column2", Type: text}, {Name: "?column?", Type: text}, {Name: "column2", Type: text},
	}}
	src, err := Emit("p", []Func{{Name: "Q", Stmt: st}})
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		if s, ok := n.(*ast.StructType); ok {
			seen := map[string]bool{}
			for _, f := range s.Fields.List {
				if name := f.Names[0].Name; seen[name] {
					t.Errorf("field %s is declared twice:\n%s", name, src)
				}
				seen[f.Names[0].Name] = true
			}
		}
		return true
	})
}

// A composite column has no Go type until rows are read at run time,
// so Emit refuses it rather than write Go that does not build.
func TestEmitRefusesComposite(t *testing.T) {
	row := Type{Name: "business", Fields: []Column{{Name: "id", Type: Type{Name: "Uuid"}}}}
	for _, nullable := range []bool{false, true} {
		st := &Statement{SQL: "select", Columns: []Column{{Name: "business", Type: row, Nullable: nullable}}}
		_, err := Emit("p", []Func{{Name: "Q", Stmt: st}})
		if want := `Q: column "business" is a row of business`; err == nil || !strings.HasPrefix(err.Error(), want) {
			t.Errorf("nullable %v: got %v, want %s", nullable, err, want)
		}
	}
}
