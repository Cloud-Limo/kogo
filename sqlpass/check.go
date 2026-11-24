package sqlpass

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// A Type is a Kogo type of spec.md section 10.4.
type Type struct {
	Name   string // Boolean, Uuid, ...; for an enum, its Postgres name in PascalCase; for a composite type, Postgres's name
	Enum   bool
	Labels []string // an enum's, in Postgres's order
	Fields []Column // a composite type's, in Postgres's order; nil for any other type
	List   bool
	Array  string // for a list whose array type Postgres does not ship, as one of a domain: Postgres's name for it
}

func (t Type) String() string {
	if t.List {
		return "List<" + t.Name + ">"
	}
	if t.Fields != nil {
		var fields []string
		for _, f := range t.Fields {
			s := f.Name + ": " + f.Type.String()
			if f.Nullable {
				s += "?"
			}
			fields = append(fields, s)
		}
		return t.Name + "(" + strings.Join(fields, ", ") + ")"
	}
	return t.Name
}

// A Column is one result column.
type Column struct {
	Name     string
	Type     Type
	Nullable bool
}

// A Table is a table or a view that Postgres's tree lists for a
// statement, and what the statement needs of it beyond select.
type Table struct {
	Name  string   // as Postgres writes it on the connection's search path
	Needs []string // of insert, update, delete and lock, in that order; none where the statement only reads it
}

// A Statement is a checked literal: the SQL to send, its signature
// and the tables it touches, by name.
type Statement struct {
	SQL     string
	Params  []Param
	Columns []Column
	Tables  []Table
	Utility string // Postgres's tag for a utility statement, as TRUNCATESTMT; empty for the rest
}

// A Checker checks literals against one database. It only prepares
// statements and reads the catalog; it never executes a literal.
type Checker struct {
	conn    *pgx.Conn
	tree    string // the parse tree of the statement last prepared
	types   map[uint32]Type
	columns map[[2]int]bool // whether (table, column) is NOT NULL
}

// Connect opens the development database named by a connection string;
// an empty one reads the PG* environment variables.
func Connect(ctx context.Context, connString string) (*Checker, error) {
	cfg, err := pgx.ParseConfig(connString)
	if err != nil {
		return nil, err
	}
	c := &Checker{types: map[uint32]Type{}, columns: map[[2]int]bool{}}
	cfg.OnNotice = func(_ *pgconn.PgConn, n *pgconn.Notice) {
		if n.Message == "parse tree:" {
			c.tree = n.Detail
		}
	}
	// Nullability is read from the parse tree, which Postgres sends as
	// a log message. Before 18 the tree does not mark grouping sets.
	cfg.RuntimeParams["debug_print_parse"] = "on"
	cfg.RuntimeParams["debug_pretty_print"] = "off"
	cfg.RuntimeParams["client_min_messages"] = "log"
	if c.conn, err = pgx.ConnectConfig(ctx, cfg); err != nil {
		return nil, err
	}
	var version int
	err = c.conn.QueryRow(ctx, "select current_setting('server_version_num')::int").Scan(&version)
	if err == nil && version < 180000 {
		err = fmt.Errorf("Postgres 18 or later is needed, this is %d", version)
	}
	if err != nil {
		c.Close(ctx)
		return nil, err
	}
	return c, nil
}

func (c *Checker) Close(ctx context.Context) { c.conn.Close(ctx) }

// Check infers the signature of one literal, whose parameters lie
// where spans says, in order. A fault in the literal is an *Error; any
// other error is a failure to ask the database.
func (c *Checker) Check(ctx context.Context, literal string, spans []Span) (*Statement, error) {
	return c.check(ctx, literal, spans, false)
}

// CheckDiscarding is Check for a call that throws the result's columns
// away, as exists and execute do: they are not typed, so they need no
// Kogo type, and the statement has none.
func (c *Checker) CheckDiscarding(ctx context.Context, literal string, spans []Span) (*Statement, error) {
	return c.check(ctx, literal, spans, true)
}

func (c *Checker) check(ctx context.Context, literal string, spans []Span, discard bool) (*Statement, error) {
	sql, params, edits, err := rewrite(literal, spans)
	if err != nil {
		return nil, err
	}
	sd, untyped, err := c.prepare(ctx, sql, len(params))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return nil, rejected(pgErr, sql, params, edits)
	} else if err != nil {
		return nil, err
	}
	query := parseTree(c.tree) // before a catalog query replaces it
	if query.tag != "QUERY" {
		return nil, errors.New("Postgres sent no parse tree for the statement, so what can be null is unknown: " +
			"connect to it directly, since a pooler can drop the session's debug_print_parse and client_min_messages")
	}
	seen := map[int]bool{}
	query.params(seen)
	for i, p := range params {
		if !seen[i+1] {
			return nil, &Error{p.Offset, "sql-parameter-syntax", fmt.Sprintf(
				"Postgres does not see %s as a parameter: it is inside a quoted string or a comment", spelling(p))}
		}
	}
	if untyped != nil {
		return nil, rejected(untyped, sql, params, edits)
	}
	for i := range params {
		params[i].Type, err = c.typeOf(ctx, sd.ParamOIDs[i])
		row, field := params[i].Type.Name, noField{}
		switch {
		case errors.As(err, &field):
			row = field.name
		case err == noKogoType("record"):
			row = "record"
		case err != nil:
			return nil, unmapped(err, params[i].Offset, "parameter "+spelling(params[i]))
		case params[i].Type.Fields == nil:
			continue
		}
		return nil, &Error{params[i].Offset, "sql-unmapped-type", fmt.Sprintf(
			"parameter %s has the composite type %s, which a parameter cannot have: pass each field as a parameter of its own",
			spelling(params[i]), row)}
	}
	st := &Statement{SQL: sql, Params: params}
	if query.num("commandType") == 6 { // CMD_UTILITY: its tree lists no table
		st.Utility = query.field("utilityStmt").tag
	}
	if st.Tables, err = c.tables(ctx, query); err != nil {
		return nil, err
	}
	if discard {
		return st, nil
	}
	exprs := outputs(query)
	for i, f := range sd.Fields {
		col := Column{Name: f.Name, Nullable: true}
		if col.Type, err = c.typeOf(ctx, f.DataTypeOID); err != nil {
			return nil, unmapped(err, 0, fmt.Sprintf("column %q", f.Name))
		}
		whole := false // a table's whole row, whose fields are NOT NULL as its columns are
		if len(exprs) == len(sd.Fields) {
			var notNull bool
			if notNull, whole, err = c.notNull(ctx, query, exprs[i], false); err != nil {
				return nil, err
			}
			col.Nullable = !notNull
		}
		if !whole {
			col.Type = loose(col.Type)
		}
		// "?column?" is Postgres's name for an unnamed column, not an override.
		if n := len(col.Name) - 1; n > 0 && (col.Name[n] == '!' || col.Name[n] == '?') && col.Name != "?column?" {
			col.Name, col.Nullable = col.Name[:n], col.Name[n] == '?'
		}
		st.Columns = append(st.Columns, col)
	}
	return st, c.wrap(ctx, st, query, sd)
}

// wrap makes the SQL that a read sends: the author's statement, never
// edited, under a select of its columns by the names Postgres gave
// them, so that a row is read by name even where the database served
// orders its columns otherwise than the one checked (a column dropped
// and added again) or has one more. A select is wrapped as a subquery,
// which Postgres plans as the statement alone; any other statement, as
// a with, the one place Postgres takes insert, update, delete and
// merge. The wrap is prepared too, so that what is checked is what is
// sent. Postgres takes a with that writes only at the top level, so a
// read whose own with writes is refused. The fields of a whole row
// read as one column are still sent in the table's order.
func (c *Checker) wrap(ctx context.Context, st *Statement, query *node, sd *pgconn.StatementDescription) error {
	if query.field("hasModifyingCTE").text() == "true" {
		return &Error{0, "sql-writing-with", "a statement that reads rows into R cannot have a with that writes, " +
			"since Kogo wraps it to read its columns by name and Postgres takes such a with only at the top level: " +
			"move the writing with into a statement of its own, run before this one"}
	}
	if st.Utility != "" || len(sd.Fields) == 0 {
		return nil
	}
	names := make([]string, len(sd.Fields))
	seen := map[string]bool{}
	for i, f := range sd.Fields {
		if seen[f.Name] {
			return &Error{0, "sql-duplicate-column", fmt.Sprintf(
				"two columns are named %s, and Kogo reads a column by its name: name each once with as, as in b.id as business_id", f.Name)}
		}
		seen[f.Name] = true
		names[i] = pgx.Identifier{f.Name}.Sanitize()
	}
	cols, body := strings.Join(names, ", "), st.SQL
	// Postgres's own end of the statement drops a trailing ; (0 is the
	// end), and the newline ends a -- comment the statement ends with.
	if n := query.num("stmt_len"); n > 0 {
		body = body[:query.num("stmt_location")+n]
	}
	sql := "select " + cols + " from (" + body + "\n) _"
	if query.num("commandType") != cmdSelect {
		sql = "with _ as (" + body + "\n) select " + cols + " from _"
	}
	wd, err := c.conn.PgConn().Prepare(ctx, "", sql, sd.ParamOIDs)
	if err != nil {
		return &Error{0, "sql-wrap", fmt.Sprintf("Postgres refused the statement as Kogo sends it, %s: %v", sql, err)}
	}
	if len(wd.Fields) != len(sd.Fields) {
		return &Error{0, "sql-wrap", fmt.Sprintf("the statement as Kogo sends it, %s, describes %d columns, the statement %d", sql, len(wd.Fields), len(sd.Fields))}
	}
	for i, f := range wd.Fields {
		if f.Name != sd.Fields[i].Name || f.DataTypeOID != sd.Fields[i].DataTypeOID {
			return &Error{0, "sql-wrap", fmt.Sprintf("the statement as Kogo sends it, %s, does not describe column %d as the statement does, %s", sql, i+1, sd.Fields[i].Name)}
		}
	}
	st.SQL = sql
	return nil
}

// tables gives the tables and views the tree lists, with what the
// statement needs of each: Postgres's own answer, read and not worked
// out. A function the statement calls is not in the tree, so what it
// writes is not here, nor is nextval's sequence.
func (c *Checker) tables(ctx context.Context, query *node) ([]Table, error) {
	seen, out := map[int]int{}, []Table(nil)
	query.perms(seen)
	for oid, perms := range seen {
		t := Table{}
		if err := c.conn.QueryRow(ctx, "select $1::oid::regclass::text", uint32(oid)).Scan(&t.Name); err != nil {
			return nil, err
		}
		for _, n := range needs {
			if perms&n.bit != 0 {
				t.Needs = append(t.Needs, n.name)
			}
		}
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b Table) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// prepare asks Postgres to prepare a statement of n parameters.
// Postgres cannot type a parameter it does not see, as one inside a
// quoted string, so each one it cannot type is declared text and the
// statement prepared again, for the parse tree to tell an unseen
// parameter from an untyped one. Postgres raises that refusal only
// once the rest of the statement is sound, so a later refusal is due
// to the declared type and the parameter was seen. untyped is the
// first such refusal, to report when the parameter was seen.
func (c *Checker) prepare(ctx context.Context, sql string, n int) (
	sd *pgconn.StatementDescription, untyped *pgconn.PgError, err error) {
	oids := make([]uint32, n) // 0: Postgres infers the type
	for {
		c.tree = ""
		sd, err = c.conn.PgConn().Prepare(ctx, "", sql, oids)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) {
			return sd, untyped, err
		}
		k := 0
		if pgErr.Code == "42P18" { // indeterminate datatype
			k, _ = strconv.Atoi(strings.TrimPrefix(numbered.FindString(pgErr.Message), "$"))
		}
		if k < 1 || k > n || oids[k-1] != 0 {
			if untyped != nil {
				pgErr = untyped
			}
			return nil, nil, pgErr
		}
		if untyped == nil {
			untyped = pgErr
		}
		oids[k-1] = 25 // text
	}
}

func spelling(p Param) string {
	if strings.IndexFunc(p.Source, func(r rune) bool { return r > 127 || !isNameByte(byte(r)) }) < 0 {
		return "$" + p.Source
	}
	return "${" + p.Source + "}"
}

var numbered = regexp.MustCompile(`\$\d+`)

// rejected turns Postgres's refusal into an Error at the place it
// names, with the parameters spelt as the literal spells them.
func rejected(e *pgconn.PgError, sql string, params []Param, edits []edit) *Error {
	msg := e.Message
	for _, more := range []string{e.Detail, e.Hint} {
		if more != "" {
			msg = strings.TrimSuffix(msg, ".") + ". " + more
		}
	}
	code := "sql-rejected"
	if e.Code == "42P18" { // indeterminate datatype
		code, msg = "sql-untyped-parameter", msg+": write a cast after it, as in $name::boolean"
	}
	at, chars := 0, 0 // Position counts characters from 1; 0 means Postgres gave none
	for i := range sql {
		if chars++; chars == int(e.Position) {
			at = origin(edits, i)
		}
	}
	if int(e.Position) == chars+1 { // just past the end, as for "at end of input"
		at = origin(edits, len(sql))
	}
	msg = numbered.ReplaceAllStringFunc(msg, func(s string) string {
		n, _ := strconv.Atoi(s[1:])
		if n < 1 || n > len(params) {
			return s
		}
		if e.Position == 0 {
			at = params[n-1].Offset
		}
		return spelling(params[n-1])
	})
	return &Error{at, code, fmt.Sprintf("%s (Postgres %s)", msg, e.Code)}
}

var scalars = map[string]string{
	"bool": "Boolean", "int4": "Int", "int8": "Long", "float8": "Double", "numeric": "Decimal",
	"text": "String", "citext": "String", "varchar": "String", "uuid": "Uuid",
	"timestamptz": "Instant", "date": "LocalDate", "time": "LocalTime", "interval": "Duration",
	"bytea": "ByteArray",
}

// noKogoType is the name of a Postgres type outside section 10.4.
type noKogoType string

func (n noKogoType) Error() string { return string(n) }

// noField is a composite type with a field, at any depth, outside
// section 10.4: its name, and why, as "whose field f has the Postgres
// type jsonb".
type noField struct{ name, why string }

func (n noField) Error() string { return n.name + ", " + n.why }

func unmapped(err error, offset int, what string) error {
	var pgType noKogoType
	var field noField
	if errors.As(err, &field) {
		return &Error{offset, "sql-unmapped-type", fmt.Sprintf(
			"%s has the Postgres type %s, which has no Kogo type: select its fields as columns, casting that one to a type of spec.md section 10.4",
			what, field)}
	}
	if !errors.As(err, &pgType) {
		return err
	}
	if pgType == "record" {
		return &Error{offset, "sql-unmapped-type", fmt.Sprintf(
			"%s is a row of the anonymous type record, as the whole row of a subquery or a CTE is: select from a table, or name the columns",
			what)}
	}
	return &Error{offset, "sql-unmapped-type", fmt.Sprintf(
		"%s has the Postgres type %s, which has no Kogo type: cast it in the statement to a type of spec.md section 10.4",
		what, string(pgType))}
}

// typeOf maps a Postgres type, asking the catalog what the type is:
// a domain maps as its base type, an array as a list of its element
// and a composite type by its fields.
func (c *Checker) typeOf(ctx context.Context, oid uint32) (Type, error) {
	if t, ok := c.types[oid]; ok {
		return t, nil
	}
	var name, kind, category, display string
	var base, elem, relid uint32
	var shipped bool
	err := c.conn.QueryRow(ctx, `select typname, typtype::text, typcategory::text, typbasetype, typelem, typrelid,
		format_type(oid, null), oid < 16384 and typnamespace = 'pg_catalog'::regnamespace
		from pg_type where oid = $1`, oid).Scan(&name, &kind, &category, &base, &elem, &relid, &display, &shipped)
	if err != nil {
		return Type{}, err
	}
	var t Type
	switch {
	case kind == "d":
		t, err = c.typeOf(ctx, base)
	case category == "A":
		t, err = c.typeOf(ctx, elem)
		if t.List { // an array of a domain over an array: a list of lists
			err = noKogoType(display)
		}
		t.List = true
		// An array of rows, a list of data classes, is not yet mapped,
		// and is named as itself, not as its element.
		var field noField
		if err == nil && t.Fields != nil || errors.As(err, &field) {
			err = noKogoType(display)
		}
	case kind == "c":
		t, err = c.composite(ctx, relid, display)
	case kind == "e":
		t = Type{Name: pascal(name), Enum: true}
		err = c.conn.QueryRow(ctx, `select array_agg(enumlabel::text order by enumsortorder) from pg_enum
			where enumtypid = $1`, oid).Scan(&t.Labels)
	case scalars[name] != "":
		t = Type{Name: scalars[name]}
	default:
		err = noKogoType(display)
	}
	if err != nil {
		return Type{}, err
	}
	// The driver knows the arrays of pg_catalog that initdb makes,
	// whose oids are below 16384, its FirstNormalObjectId; it reads any
	// other, an array of a domain, a domain over an array, or one of
	// information_schema's domains, once told of it by name.
	if t.Array = ""; t.List && !shipped {
		t.Array = display
	}
	c.types[oid] = t
	return t, nil
}

// composite reads the fields of a composite type, whose relation is
// relid: a table's row type, whose fields are NOT NULL where its
// columns are, or one made with create type, which Postgres cannot
// declare NOT NULL. A field's own fields may all be null, since no
// table's constraint covers a value stored in a field.
func (c *Checker) composite(ctx context.Context, relid uint32, display string) (Type, error) {
	rows, _ := c.conn.Query(ctx, `select a.attname::text, a.atttypid, `+validNotNull+` from pg_attribute a
		where a.attrelid = $1 and a.attnum > 0 and not a.attisdropped order by a.attnum`, relid)
	type attribute struct {
		Name    string
		OID     uint32
		NotNull bool
	}
	attrs, err := pgx.CollectRows(rows, pgx.RowToStructByPos[attribute])
	if err != nil {
		return Type{}, err
	}
	if len(attrs) == 0 { // no data class has no properties
		return Type{}, noKogoType(display)
	}
	t := Type{Name: display, Fields: []Column{}}
	for _, a := range attrs {
		ft, err := c.typeOf(ctx, a.OID)
		var pgType noKogoType
		var field noField
		if errors.As(err, &pgType) || errors.As(err, &field) {
			return Type{}, noField{display, fmt.Sprintf("whose field %s has the Postgres type %s", a.Name, err)}
		} else if err != nil {
			return Type{}, err
		}
		t.Fields = append(t.Fields, Column{Name: a.Name, Type: loose(ft), Nullable: !a.NotNull})
	}
	return t, nil
}

// loose gives t with every field of a composite type, at any depth,
// nullable: the fields of any value but a table's whole row.
func loose(t Type) Type {
	if t.Fields == nil {
		return t
	}
	fields := make([]Column, len(t.Fields))
	for i, f := range t.Fields {
		fields[i] = Column{Name: f.Name, Type: loose(f.Type), Nullable: true}
	}
	t.Fields = fields
	return t
}

// pascal turns snake_case into PascalCase.
func pascal(s string) string {
	var b strings.Builder
	for _, part := range strings.Split(s, "_") {
		if part != "" {
			b.WriteString(strings.ToUpper(part[:1]) + part[1:])
		}
	}
	return b.String()
}

// validNotNull is whether the column of pg_attribute a has a validated
// NOT NULL constraint. Postgres 18 also sets attnotnull for one NOT VALID.
const validNotNull = `a.attnotnull and not exists (
	select from pg_constraint k
	where k.conrelid = a.attrelid and k.contype = 'n' and k.conkey = array[a.attnum] and not k.convalidated)`

// columnNotNull reports whether a table's column has a validated NOT
// NULL constraint.
func (c *Checker) columnNotNull(ctx context.Context, table, column int) (bool, error) {
	key := [2]int{table, column}
	if v, ok := c.columns[key]; ok {
		return v, nil
	}
	var v bool
	err := c.conn.QueryRow(ctx, `select `+validNotNull+` from pg_attribute a
		where a.attrelid = $1 and a.attnum = $2`, uint32(table), int16(column)).Scan(&v)
	if err != nil {
		return false, err
	}
	c.columns[key] = v
	return v, nil
}
