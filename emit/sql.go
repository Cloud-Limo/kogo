package emit

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Cloud-Limo/kogo/check"
	"github.com/Cloud-Limo/kogo/sqlpass"
	"github.com/Cloud-Limo/kogo/syntax"
)

// sql writes one of the five calls of spec.md 10.2 as a call of the
// prelude's Go: the Tx, the statement as Postgres takes it, and what
// each of its parameters binds, in the order written. A call that
// reads rows also passes a function that reads one. Where a column,
// a field of one, or a parameter is of an array type that Postgres
// does not ship, the Tx is one whose driver knows the type.
func (e *emitter) sql(x *syntax.Call) string {
	sel := x.Fun.(*syntax.Select)
	q := e.info.SQL[x.Args[0].Value.(*syntax.SQL)]
	kogo := e.use(preludePath + "kogo")
	args := []string{e.expr(sel.X), strconv.Quote(q.Stmt.SQL)}
	var known []string // the types the driver must be told of
	for i, p := range q.Stmt.Params {
		if enums(p.Type) {
			e.notYet(q.Args[i], "a parameter that is a list of an enum", "cast it in the statement, as $name::text[]::enum_type[], and pass a List<String> of the constants' names")
		}
		if p.Type.Array != "" {
			known = append(known, strconv.Quote(p.Type.Array))
		}
	}
	if q.Row != nil {
		for _, c := range q.Stmt.Columns {
			if enums(c.Type) {
				e.notYet(x, "column "+c.Name+", which holds a list of an enum,", "cast the list in the statement, as kinds::text[], and read a List<String?>")
			}
			known = arrays(known, c.Type)
		}
	}
	if known != nil {
		args[0] = fmt.Sprintf("%s.Tx_Known(%s, %s)", kogo, args[0], strings.Join(known, ", "))
	}
	if q.Row != nil {
		row := e.info.Types[x] // R, or queryOne's R?
		if sel.Name == "query" {
			row = q.Row
		}
		args = append(args, e.reader(q, row))
	}
	for _, a := range q.Args {
		args = append(args, e.expr(a))
	}
	return kogo + ".Tx_" + export(sel.Name) + "(" + strings.Join(args, ", ") + ")"
}

// enums reports whether a value of the type t is, or holds in a
// field, a list of an enum: the driver knows no enum's array type
// until it is told of it, as it is told of a composite type.
func enums(t sqlpass.Type) bool {
	return t.Enum && t.List || slices.ContainsFunc(t.Fields, func(f sqlpass.Column) bool { return enums(f.Type) })
}

// arrays adds to known the array types that the driver must be told
// of to read a value of the type t: t's own, or those of its fields,
// which come as columns of their own (sqlpass's wrap).
func arrays(known []string, t sqlpass.Type) []string {
	if t.Array != "" {
		known = append(known, strconv.Quote(t.Array))
	}
	for _, f := range t.Fields {
		known = arrays(known, f.Type)
	}
	return known
}

// reader writes the function that reads one row of q as the type to:
// each column is scanned into a variable of its own, and R is built
// from them by the checker's paths.
func (e *emitter) reader(q *check.Query, to check.Type) string {
	dests := make([]string, len(q.Stmt.Columns))
	body := e.inner(func() {
		code, _ := e.fill(q, q.Row, nil, false, dests)
		e.w("scan(%s)", strings.Join(dests, ", "))
		e.w("return %s", e.convert(code, q.Row, to))
	})
	return "func(scan " + e.use(preludePath+"kogo") + ".Scan) " + e.typ(to) + " {\n" + body + e.dir() + "}"
}

// bare gives T for a T or a T?, and whether it was a T?.
func bare(t check.Type) (check.Type, bool) {
	if n, ok := t.(*check.Nullable); ok {
		return n.Elem, true
	}
	return t, false
}

// fill declares what the columns of the value at path, of the type
// t, are scanned into: R itself, or a property on the way down from
// it. It gives the Go of the value, and a test for null of each of its
// columns that is null only through an outer join. The value is its
// one column's, or a data class built from the columns of its
// properties (spec.md 10.3); a nullable one is null when those of its
// columns are, which kogo check saw it has, and tells its holder of
// none. absent is whether a nullable data class holds the value, so
// that its columns may be null whatever t says.
func (e *emitter) fill(q *check.Query, t check.Type, path []int, absent bool, dests []string) (code string, nulls []string) {
	if j := slices.IndexFunc(q.Paths, func(p []int) bool { return slices.Equal(p, path) }); j >= 0 {
		c := q.Stmt.Columns[j]
		code, nulls = e.column(c.Name, c.Type, t, absent, &dests[j])
		if !c.Joined {
			nulls = nil
		}
		return code, nulls
	}
	elem, null := bare(t)
	d := elem.(*check.Named)
	var props []string
	for i, p := range d.Decl.Props {
		code, ns := e.fill(q, check.Subst(p.Type, d.Decl.Params, d.Args), append(slices.Clip(path), i), absent || null, dests)
		props = append(props, export(p.Name)+": "+code)
		nulls = append(nulls, ns...)
	}
	code = e.typ(d) + "{" + strings.Join(props, ", ") + "}"
	if null {
		code = fmt.Sprintf("func() %s { if %s { return nil }; return %s.Ptr(%s) }()", e.typ(t), strings.Join(nulls, " && "), e.rt(), code)
		nulls = nil
	}
	return code, nulls
}

// column declares what one column, or one field of a composite type,
// is scanned into, and sets dest to point there. It gives the Go of
// its value as the type t, and a test for null where it may be null.
// A column that t or absent lets be null is read as a pointer; a list
// is read by the prelude's Array, as a List<T?>, the one kogo check
// takes, and is null for a NULL; any other is read as it is, and the
// driver refuses a null, as one in a column named "name!" (spec.md
// 10.5). A composite type comes as a column that says whether it is
// null and then one for each field (sqlpass's wrap), in the order kogo
// check saw, each read into the property of its name: null as a
// whole, and never field by field. Where it is null every field's
// column is, so each is read as a pointer and only looked at where the
// whole is there.
func (e *emitter) column(name string, ct sqlpass.Type, t check.Type, absent bool, dest *string) (code string, nulls []string) {
	kogo := e.use(preludePath + "kogo")
	elem, null := bare(t)
	v := e.temp()
	switch {
	case ct.Fields != nil:
		d := elem.(*check.Named)
		e.w("var %s bool", v)
		fields := make([]string, len(ct.Fields))
		var props []string
		for _, p := range d.Decl.Props {
			i := slices.IndexFunc(ct.Fields, func(f sqlpass.Column) bool { return f.Name == check.Snake(p.Name) })
			code, _ := e.column(name+"."+ct.Fields[i].Name, ct.Fields[i].Type, check.Subst(p.Type, d.Decl.Params, d.Args), true, &fields[i])
			props = append(props, export(p.Name)+": "+code)
		}
		*dest = "&" + v + ", " + strings.Join(fields, ", ")
		code = fmt.Sprintf("func() *%s { if %s { return nil }; return %s.Ptr(%s{%s}) }()", e.typ(d), v, e.rt(), e.typ(d), strings.Join(props, ", "))
		nulls = []string{v}
	case ct.List:
		of, _ := bare(elem.(*check.Named).Args[0])
		e.w("var %s %s.Array[%s]", v, kogo, e.typ(of))
		*dest, code, nulls = "&"+v, v+".Nullable()", []string{v + ".Nullable() == nil"}
	case null || absent:
		e.w("var %s *%s", v, e.typ(elem))
		*dest, code, nulls = "&"+v, v, []string{v + " == nil"}
	default:
		e.w("var %s %s", v, e.typ(elem))
		*dest = "&" + v
		return v, nil
	}
	if !null {
		code = fmt.Sprintf("%s.NotNull(%s, %q)", kogo, code, name)
	}
	return code, nulls
}
