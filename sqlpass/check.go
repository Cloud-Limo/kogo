package sqlpass

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// A Type is a Kogo type of spec.md section 10.4.
type Type struct {
	Name string // Boolean, Uuid, ...; for an enum, its Postgres name in PascalCase
	Enum bool
	List bool
}

func (t Type) String() string {
	if t.List {
		return "List<" + t.Name + ">"
	}
	return t.Name
}

// A Column is one result column.
type Column struct {
	Name     string
	Type     Type
	Nullable bool
}

// A Statement is a checked literal: the SQL to send and its signature.
type Statement struct {
	SQL     string
	Params  []Param
	Columns []Column
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

// Check infers the signature of one literal. A fault in the literal
// is an *Error; any other error is a failure to ask the database.
func (c *Checker) Check(ctx context.Context, literal string) (*Statement, error) {
	sql, params, edits, err := rewrite(literal)
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
		if params[i].Type, err = c.typeOf(ctx, sd.ParamOIDs[i]); err != nil {
			return nil, unmapped(err, params[i].Offset, "parameter "+spelling(params[i]))
		}
	}
	exprs := outputs(query)
	st := &Statement{SQL: sql, Params: params}
	for i, f := range sd.Fields {
		col := Column{Name: f.Name, Nullable: true}
		if col.Type, err = c.typeOf(ctx, f.DataTypeOID); err != nil {
			return nil, unmapped(err, 0, fmt.Sprintf("column %q", f.Name))
		}
		if len(exprs) == len(sd.Fields) {
			notNull, err := c.notNull(ctx, query, exprs[i])
			if err != nil {
				return nil, err
			}
			col.Nullable = !notNull
		}
		// "?column?" is Postgres's name for an unnamed column, not an override.
		if n := len(col.Name) - 1; n > 0 && (col.Name[n] == '!' || col.Name[n] == '?') && col.Name != "?column?" {
			col.Name, col.Nullable = col.Name[:n], col.Name[n] == '?'
		}
		st.Columns = append(st.Columns, col)
	}
	return st, nil
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
}

// noKogoType is the name of a Postgres type outside section 10.4.
type noKogoType string

func (n noKogoType) Error() string { return string(n) }

func unmapped(err error, offset int, what string) error {
	var pgType noKogoType
	if !errors.As(err, &pgType) {
		return err
	}
	return &Error{offset, "sql-unmapped-type", fmt.Sprintf(
		"%s has the Postgres type %s, which has no Kogo type: cast it in the statement to a type of spec.md section 10.4",
		what, string(pgType))}
}

// typeOf maps a Postgres type, asking the catalog what the type is:
// a domain maps as its base type and an array as a list of its element.
func (c *Checker) typeOf(ctx context.Context, oid uint32) (Type, error) {
	if t, ok := c.types[oid]; ok {
		return t, nil
	}
	var name, kind, category, display string
	var base, elem uint32
	err := c.conn.QueryRow(ctx, `select typname, typtype::text, typcategory::text, typbasetype, typelem,
		format_type(oid, null) from pg_type where oid = $1`, oid).Scan(&name, &kind, &category, &base, &elem, &display)
	if err != nil {
		return Type{}, err
	}
	var t Type
	switch {
	case kind == "d":
		t, err = c.typeOf(ctx, base)
	case category == "A":
		t, err = c.typeOf(ctx, elem)
		t.List = true
	case kind == "e":
		t = Type{Name: pascal(name), Enum: true}
	case scalars[name] != "":
		t = Type{Name: scalars[name]}
	default:
		err = noKogoType(display)
	}
	if err != nil {
		return Type{}, err
	}
	c.types[oid] = t
	return t, nil
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

// columnNotNull reports whether a table's column has a validated NOT
// NULL constraint. Postgres 18 also sets attnotnull for one NOT VALID.
func (c *Checker) columnNotNull(ctx context.Context, table, column int) (bool, error) {
	key := [2]int{table, column}
	if v, ok := c.columns[key]; ok {
		return v, nil
	}
	var v bool
	err := c.conn.QueryRow(ctx, `select a.attnotnull and not exists (
			select from pg_constraint k
			where k.conrelid = a.attrelid and k.contype = 'n' and k.conkey = array[a.attnum] and not k.convalidated)
		from pg_attribute a where a.attrelid = $1 and a.attnum = $2`, uint32(table), int16(column)).Scan(&v)
	if err != nil {
		return false, err
	}
	c.columns[key] = v
	return v, nil
}
