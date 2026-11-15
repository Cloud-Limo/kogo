package emit

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"kogo/check"
	"kogo/sqlpass"
	"kogo/syntax"
)

// sql writes one of the five calls of spec.md 10.2 as a call of the
// prelude's Go: the Tx, the statement as Postgres takes it, and what
// each of its parameters binds, in the order written. A call that
// reads rows also passes a function that reads one, and where a
// column is of a composite type, a Tx whose driver knows the type.
func (e *emitter) sql(x *syntax.Call) string {
	sel := x.Fun.(*syntax.Select)
	q := e.info.SQL[x.Args[0].Value.(*syntax.SQL)]
	kogo := e.use(preludePath + "kogo")
	args := []string{e.expr(sel.X), strconv.Quote(q.Stmt.SQL)}
	for i, p := range q.Stmt.Params {
		if enums(p.Type) {
			e.notYet(q.Args[i], "a parameter that is a list of an enum", "cast it in the statement, as $name::text[]::enum_type[], and pass a List<String> of the constants' names")
		}
	}
	if q.Row != nil {
		var composites []string
		for _, c := range q.Stmt.Columns {
			if enums(c.Type) {
				e.notYet(x, "column "+c.Name+", which holds a list of an enum,", "cast the list in the statement, as kinds::text[], and read a List<String>")
			}
			if c.Type.Fields != nil {
				composites = append(composites, strconv.Quote(c.Type.Name))
			}
		}
		if composites != nil {
			args[0] = fmt.Sprintf("%s.Tx_Known(%s, %s)", kogo, args[0], strings.Join(composites, ", "))
		}
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

// reader writes the function that reads one row of q as the type to:
// each column is scanned into a variable of its own, and R is built
// from them by the checker's paths.
func (e *emitter) reader(q *check.Query, to check.Type) string {
	dests := make([]string, len(q.Stmt.Columns))
	body := e.capture(func() {
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
// it. It gives the Go of the value, and a test of each of its columns
// for null. The value is its one column's, or a data class built from
// the columns of its properties (spec.md 10.3); a nullable one is
// null when all of them are. absent is whether a nullable data class
// holds the value, so that its columns may be null whatever t says.
func (e *emitter) fill(q *check.Query, t check.Type, path []int, absent bool, dests []string) (code string, nulls []string) {
	if j := slices.IndexFunc(q.Paths, func(p []int) bool { return slices.Equal(p, path) }); j >= 0 {
		c := q.Stmt.Columns[j]
		return e.column(c.Name, c.Type, t, absent, &dests[j])
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
	}
	return code, nulls
}

// column declares what one column, or one field of a composite type,
// is scanned into, and sets dest to point there. It gives the Go of
// its value as the type t, and a test for null where it may be null.
// A column that t or absent lets be null is read as a pointer; any
// other is read as it is, and the driver refuses a null, as one in a
// column named "name!" (spec.md 10.5). A composite type is read field
// by field, in Postgres's order, into the properties of their names:
// null as a whole, and never absent field by field.
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
			code, _ := e.column(name+"."+ct.Fields[i].Name, ct.Fields[i].Type, check.Subst(p.Type, d.Decl.Params, d.Args), false, &fields[i])
			props = append(props, export(p.Name)+": "+code)
		}
		*dest = fmt.Sprintf("%s.Composite{Null: &%s, Fields: []any{%s}}", kogo, v, strings.Join(fields, ", "))
		code, nulls = fmt.Sprintf("%s.Row(%s, %s{%s})", kogo, v, e.typ(d), strings.Join(props, ", ")), []string{v}
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
