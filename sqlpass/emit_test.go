package sqlpass

import (
	"go/ast"
	"go/parser"
	"go/token"
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
