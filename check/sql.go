package check

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"kogo/sqlpass"
	"kogo/syntax"
)

// Signatures gives a sql literal's signature: the SQL pass's, or in a
// test, one of the test's own. A *sqlpass.Error is a fault in the
// literal; any other error, a failure to ask.
type Signatures func(*syntax.SQL) (*sqlpass.Statement, error)

// Signer gives the SQL pass's signatures, from the database db
// connects to: a literal's parameters lie where the parser found them,
// and the columns of exists and execute are not typed.
func Signer(ctx context.Context, db *sqlpass.Checker) Signatures {
	return func(lit *syntax.SQL) (*sqlpass.Statement, error) {
		var spans []sqlpass.Span
		for _, p := range lit.Params {
			spans = append(spans, sqlpass.Span{Start: p.Offset - lit.TextPos.Offset, End: p.End.Offset - lit.TextPos.Offset})
		}
		if !lit.Typed() {
			return db.CheckDiscarding(ctx, lit.Text, spans)
		}
		return db.Check(ctx, lit.Text, spans)
	}
}

// A Query is a sql literal as the checker matched it with the Kogo
// around it (spec.md section 10).
type Query struct {
	Stmt *sqlpass.Statement // what to send and its signature; nil when it was refused
	Row  Type               // R of query, queryOne and querySingle; nil for exists and execute
	// Args holds what each parameter of Stmt binds: $1 binds Args[0].
	Args []syntax.Expr
	// Paths holds, for each column of Stmt, the property it fills:
	// the indexes of the properties from R down, as [2 0] for the
	// first property of R's third. A scalar R is filled by its one
	// column, whose path is empty. A column of a composite type fills
	// a data class, whose properties take the fields of their names.
	Paths [][]int
}

// The types of section 10.4 that live in kogo.time; the others are
// kogo's, the builtins among them.
var timeTypes = map[string]bool{"Instant": true, "LocalDate": true, "LocalTime": true, "Duration": true}

// sql checks one of the five calls of spec.md 10.2 and its literal:
// the receiver is a Tx, each parameter's type maps to the type
// Postgres infers for it, and the columns fill R.
func (c *checker) sql(e *syntax.Call, lit *syntax.SQL) Type {
	sel := e.Fun.(*syntax.Select)
	tx := &Named{Decl: c.prelude("Tx")}
	if rt := c.expr(sel.X, nil); !fits(rt, tx) {
		c.mismatch(sel.X, rt, tx)
	}
	q := &Query{}
	c.info.SQL[lit] = q
	var result Type
	switch sel.Name {
	case "exists":
		result = tBoolean
	case "execute":
		result = tInt
	default:
		q.Row = c.typ(e.TypeArgs[0])
		result = map[string]Type{"query": &Named{c.prelude("List"), []Type{q.Row}}, "queryOne": nullable(q.Row), "querySingle": q.Row}[sel.Name]
	}
	types := make([]Type, len(lit.Params))
	for i, p := range lit.Params {
		types[i] = c.expr(p.Expr, nil)
	}
	if c.signer == nil {
		c.errorf(lit, "sql-check", "there is no database to check this literal against")
		return result
	}
	st, err := c.signer(lit)
	var fault *sqlpass.Error
	if errors.As(err, &fault) {
		c.errorf(position(lit, fault.Offset), fault.Code, "%s", fault.Message)
		return result
	} else if err != nil {
		c.errorf(lit, "sql-check", "this literal could not be checked: %v", err)
		return result
	}
	q.Stmt, q.Args = st, make([]syntax.Expr, len(st.Params))
	c.params(lit, q, types)
	if q.Row != nil && q.Row != unknown {
		c.row(lit, q)
	}
	return result
}

// position gives where a byte offset of a literal's text lies.
func position(lit *syntax.SQL, offset int) syntax.Pos {
	before, at := lit.Text[:offset], lit.TextPos
	if i := strings.LastIndexByte(before, '\n'); i >= 0 {
		return syntax.Pos{Offset: at.Offset + offset, Line: at.Line + strings.Count(before, "\n"), Column: utf8.RuneCountInString(before[i+1:]) + 1}
	}
	return syntax.Pos{Offset: at.Offset + offset, Line: at.Line, Column: at.Column + utf8.RuneCountInString(before)}
}

// params checks each parameter's Kogo type against the type Postgres
// inferred for it: they map, a T? standing for a T (spec.md 10.4 and
// 10.5). A name written twice is one parameter, checked once.
func (c *checker) params(lit *syntax.SQL, q *Query, types []Type) {
	names := map[string]int{}
	n := 0
	for i, p := range lit.Params {
		j, seen := names[p.Name]
		if !seen || p.Name == "" {
			j, n = n, n+1
			names[p.Name] = j
		}
		if j >= len(q.Stmt.Params) || q.Args[j] != nil {
			continue
		}
		q.Args[j] = p.Expr
		pt := q.Stmt.Params[j].Type
		if t := nonNull(types[i]); !is(t, tNothing) && !c.maps(t, pt) {
			spelt := lit.Text[p.Offset-lit.TextPos.Offset : p.End.Offset-lit.TextPos.Offset]
			c.errorf(p, "sql-parameter-type", "%s is a %s, and Postgres takes a %s there: pass a %s, or cast it in the statement, as %s::type",
				spelt, types[i], kogoName(pt), kogoName(pt), spelt)
		}
	}
}

// row matches the columns to R (spec.md 10.3): a data class's
// properties by name, or a scalar's one column. Each data class filled
// is built as its constructor would build it, and needs what a call of
// the constructor needs.
func (c *checker) row(lit *syntax.SQL, q *Query) {
	cols := q.Stmt.Columns
	q.Paths = make([][]int, len(cols))
	d := dataClass(q.Row)
	_, null := q.Row.(*Nullable)
	switch {
	case len(cols) == 0:
		c.errorf(lit, "sql-columns", "this statement gives no columns: call execute, or add returning")
	case d != nil && null:
		c.errorf(lit, "sql-columns", "a row is never null: write %s, not %s", d, q.Row)
	case d != nil:
		// Where a data class had no column at all, a column left over
		// of the name of one of its properties is likely its own, as b.*
		// gives a business's: its error says what to select instead.
		missed := c.fill(lit, d, "", nil, false, cols, q.Paths)
		for i, path := range q.Paths {
			owned := slices.ContainsFunc(missed, func(m *Named) bool {
				return slices.ContainsFunc(m.Decl.Props, func(p *Var) bool { return Snake(p.Name) == cols[i].Name })
			})
			if path == nil && !owned {
				c.errorf(lit, "sql-column-unused", "column %s fills no property of %s: add a property %s, or leave the column out",
					cols[i].Name, d, camel(cols[i].Name))
			}
		}
	case len(cols) > 1:
		c.errorf(lit, "sql-columns", "this statement gives %d columns, and a %s is one: read them into a data class with a property for each, or select one column", len(cols), q.Row)
	default:
		q.Paths[0] = []int{}
		c.column(lit, cols[0], q.Row, false, "the result", "")
	}
}

// dataClass gives t, or what t? holds, when it is a data class.
func dataClass(t Type) *Named {
	if n, ok := nonNull(t).(*Named); ok && n.Decl.Kind == "data class" {
		return n
	}
	return nil
}

// fill matches the columns to the properties of the data class d:
// property legalName takes column legal_name, and a property that is
// a data class takes the column of its name, of a composite type, or
// else the columns that begin with its name and an underscore. prefix
// is what the columns of d begin with and path the properties that
// lead to d from R. Inside a nullable data class, a column that may be
// null fills a property that may not, since all of them are null where
// it is absent. missed holds the data classes that had no column at
// all. A column that a property of d takes by name, as business_id,
// does not count as one with the prefix of a sibling, business.
func (c *checker) fill(lit *syntax.SQL, d *Named, prefix string, path []int, absent bool, cols []sqlpass.Column, paths [][]int) (missed []*Named) {
	c.builds(lit, d.Decl)
	b := bind(d.Decl.Params, d.Args)
	prefixed := func(name string) bool {
		return slices.ContainsFunc(cols, func(col sqlpass.Column) bool {
			return strings.HasPrefix(col.Name, name) &&
				!slices.ContainsFunc(d.Decl.Props, func(p *Var) bool { return prefix+Snake(p.Name) == col.Name })
		})
	}
	for i, p := range d.Decl.Props {
		t, name, here := subst(p.Type, b), prefix+Snake(p.Name), append(slices.Clip(path), i)
		what := d.Decl.Name + "." + p.Name
		n := dataClass(t)
		j := slices.IndexFunc(cols, func(col sqlpass.Column) bool { return col.Name == name })
		switch {
		case j < 0 && n != nil && !prefixed(name+"_"):
			c.errorf(lit, "sql-column-missing", "no column fills %s, a %s: select a table's whole row as %s, as in select b as %s, or columns with the prefix %s_",
				what, t, name, name, name)
			missed = append(missed, n)
		case j < 0 && n != nil && len(path) > 8:
			c.errorf(lit, "sql-columns", "%s holds data classes nested too deep to fill from columns", d)
			return missed
		case j < 0 && n != nil:
			_, null := t.(*Nullable)
			missed = append(missed, c.fill(lit, n, name+"_", here, absent || null, cols, paths)...)
		case j < 0:
			c.errorf(lit, "sql-column-missing", "no column fills %s: select one named %s, with as %s if need be", what, name, name)
		case paths[j] != nil:
			c.errorf(lit, "sql-column-missing", "column %s would fill two properties, %s among them: rename one of them", name, what)
		default:
			paths[j] = here
			c.column(lit, cols[j], t, absent, what, "")
		}
	}
	return missed
}

// column checks that a column, or a field of column of, can fill a
// property, or the result, of type t: its type maps to t's, and it
// is not null unless t is a T? or the column lies in a nullable data
// class (spec.md 10.4, 10.5). A composite type fills a data class
// field by field.
func (c *checker) column(lit *syntax.SQL, col sqlpass.Column, t Type, absent bool, what, of string) {
	_, null := t.(*Nullable)
	n := dataClass(t)
	subject, cast, assert := "column "+col.Name, ", or cast the column in the statement", fmt.Sprintf(", or if the column is never null, name it as \"%s!\"", col.Name)
	if of != "" { // Postgres takes neither a cast nor a name of a field
		subject, cast, assert = "field "+col.Name+" of column "+of, "", ""
	}
	switch {
	case t == unknown:
	case col.Type.Fields != nil && n == nil:
		c.errorf(lit, "sql-column-type", "%s is a row of %s, and fills %s, a %s: make that a data class with a property for each of its fields, or select them as columns",
			subject, col.Type.Name, what, t)
	case col.Type.Fields == nil && !c.maps(nonNull(t), col.Type):
		c.errorf(lit, "sql-column-type", "%s is a %s, and fills %s, a %s: make that a %s%s",
			subject, kogoName(col.Type), what, t, kogoName(col.Type), cast)
	case col.Nullable && !null && !absent:
		c.errorf(lit, "sql-nullable", "%s may be null, and fills %s, a %s: make that a %s?%s", subject, what, t, t, assert)
	}
	if col.Type.Fields != nil && n != nil {
		if of != "" {
			col.Name = of + "." + col.Name
		}
		c.fields(lit, col, n)
	}
}

// fields matches the fields of col, of a composite type, to the
// properties of the data class d by name, as fill matches columns. The
// row is null as a whole, never field by field, so where it is there a
// field that may be null fills only a property that may be.
func (c *checker) fields(lit *syntax.SQL, col sqlpass.Column, d *Named) {
	c.builds(lit, d.Decl)
	b := bind(d.Decl.Params, d.Args)
	used := make([]bool, len(col.Type.Fields))
	for _, p := range d.Decl.Props {
		name := Snake(p.Name)
		j := slices.IndexFunc(col.Type.Fields, func(f sqlpass.Column) bool { return f.Name == name })
		if j < 0 {
			c.errorf(lit, "sql-field-missing", "no field of column %s fills %s.%s: %s has no field %s; name the property after one of its fields, or select columns",
				col.Name, d.Decl.Name, p.Name, col.Type.Name, name)
			continue
		}
		if used[j] {
			c.errorf(lit, "sql-field-missing", "field %s of column %s would fill two properties, %s.%s among them: rename one of them",
				name, col.Name, d.Decl.Name, p.Name)
			continue
		}
		used[j] = true
		c.column(lit, col.Type.Fields[j], subst(p.Type, b), false, d.Decl.Name+"."+p.Name, col.Name)
	}
	for j, f := range col.Type.Fields {
		if !used[j] {
			c.errorf(lit, "sql-field-unused", "field %s of column %s fills no property of %s: add a property %s, or select the columns you want",
				f.Name, col.Name, d, camel(f.Name))
		}
	}
}

// maps reports whether a Postgres type maps to the Kogo type t, which
// is not a T? (spec.md 10.4). An enum maps by its constants; a list's
// elements may be null.
func (c *checker) maps(t Type, pt sqlpass.Type) bool {
	n, ok := t.(*Named)
	switch {
	case t == unknown:
		return true
	case !ok:
		return false
	case pt.List:
		pt.List = false
		return n.Decl == c.prelude("List") && c.maps(nonNull(n.Args[0]), pt)
	case pt.Enum:
		names := []string{}
		for _, e := range n.Decl.Entries {
			names = append(names, e.Name)
		}
		slices.Sort(names)
		return n.Decl.Kind == "enum class" && slices.Equal(names, slices.Sorted(slices.Values(pt.Labels)))
	}
	home := "kogo"
	if timeTypes[pt.Name] {
		home = "kogo.time"
	}
	return n.Decl.Name == pt.Name && n.Decl.Pkg == home && (n.Decl.Kind == "builtin" || n.Decl.Kind == "external class")
}

// kogoName names a Postgres type as Kogo writes it, an enum with its
// constants.
func kogoName(pt sqlpass.Type) string {
	if !pt.Enum {
		return pt.String()
	}
	s := fmt.Sprintf("enum class %s { %s }", pt.Name, strings.Join(pt.Labels, ", "))
	if pt.List {
		return "List<" + s + ">"
	}
	return s
}

// Snake gives the name of the column, or of the field of a composite
// type, that fills a property: legalName is legal_name.
func Snake(name string) string {
	var b strings.Builder
	for i, r := range name {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

// camel gives the property that a column fills: legal_name fills
// legalName.
func camel(column string) string {
	parts := strings.Split(column, "_")
	for i, p := range parts[1:] {
		if p != "" {
			parts[i+1] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}
