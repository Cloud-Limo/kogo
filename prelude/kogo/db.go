package kogo

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Db, Tx and the Go behind spec.md section 10: the five calls, db.tx
// and tx.atomically, over the Postgres driver. Each handle holds the
// request's context (doc/design.md, "The emitter"). What Postgres or
// the driver refuses is a bug (section 8): a panic whose value is the
// driver's error.

// A Db is a connection pool, as one request sees it.
type Db struct {
	ctx  context.Context
	pool *pgxpool.Pool
}

// NewDb is how a driver makes a Db: from its pool and the context of
// the request that the Db serves.
func NewDb(ctx context.Context, pool *pgxpool.Pool) Db { return Db{ctx, pool} }

// A Tx is an open transaction.
type Tx struct {
	ctx context.Context
	tx  pgx.Tx
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// Db_Tx_Boolean_TxToA is db.tx { } where the enclosing fun returns no
// Either: it commits when block returns and rolls back when block
// panics, with a failure of a raise scope around it or with a bug.
func Db_Tx_Boolean_TxToA[A any](this Db, serializable *bool, block func(Tx) A) A {
	if serializable != nil && *serializable {
		return SerializableTx(this, func(*pgconn.PgError) bool { return true }, block)
	}
	a, _ := attempt(this, pgx.TxOptions{}, block)
	return a
}

// SerializableTx is db.tx(serializable = true) { }, for the server
// too, which limits its reruns: block is run again from the start for
// as long as Postgres ends it with a serialization failure or a
// deadlock and again, told which, says to go on. Where again says not
// to, Postgres's error is a panic.
func SerializableTx[A any](db Db, again func(conflict *pgconn.PgError) bool, block func(Tx) A) A {
	for {
		a, conflict := attempt(db, pgx.TxOptions{IsoLevel: pgx.Serializable}, block)
		if conflict == nil {
			return a
		}
		if !again(conflict) {
			panic(conflict)
		}
	}
}

// ReadOnlyTx runs block in a transaction of its own that Postgres
// holds to reading, and rolls it back: the server's, for a list's
// check (spec.md 15.3).
func ReadOnlyTx[A any](db Db, block func(Tx) A) A {
	tx := must(db.pool.BeginTx(db.ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly}))
	defer tx.Rollback(db.ctx)
	decimals(tx.Conn().TypeMap())
	return block(Tx{db.ctx, tx})
}

// attempt runs block in one transaction. It gives the conflict that
// ended the transaction where it is to be run again.
func attempt[A any](db Db, opts pgx.TxOptions, block func(Tx) A) (a A, conflict *pgconn.PgError) {
	tx := must(db.pool.BeginTx(db.ctx, opts))
	decimals(tx.Conn().TypeMap())
	done := false
	defer func() {
		if done {
			return
		}
		p := recover()
		tx.Rollback(db.ctx)
		if err, ok := p.(error); !ok || opts.IsoLevel != pgx.Serializable || !errors.As(err, &conflict) ||
			conflict.Code != "40001" && conflict.Code != "40P01" {
			panic(p)
		}
	}()
	a = block(Tx{db.ctx, tx})
	must(0, tx.Commit(db.ctx))
	done = true
	return a, nil
}

// Db_Tx_Boolean_RaiseETxToA is db.tx { } where the enclosing fun
// returns an Either<E, ...>: block is a raise scope for E, given its
// handle as either's block is, and a failure rolls back and is the
// Left that db.tx gives.
func Db_Tx_Boolean_RaiseETxToA[E, A any](this Db, serializable *bool, block func(Raise[E], Tx) A) Either[E, A] {
	return Either_RaiseEToA(func(r Raise[E]) A {
		return Db_Tx_Boolean_TxToA(this, serializable, func(tx Tx) A { return block(r, tx) })
	})
}

// Tx_Atomically is tx.atomically { }: block runs in a savepoint, and
// a panic in it, a failure or a bug, rolls back to the savepoint on
// its way out.
func Tx_Atomically[A any](this Tx, block func() A) A {
	savepoint := must(this.tx.Begin(this.ctx))
	done := false
	defer func() {
		if !done {
			savepoint.Rollback(this.ctx)
		}
	}()
	a := block()
	must(0, savepoint.Commit(this.ctx))
	done = true
	return a
}

// RolledBack runs block in a transaction that is rolled back when
// block ends, however it ends: kogo test's, one for each test.
func RolledBack(db Db, block func(Tx)) {
	tx := must(db.pool.Begin(db.ctx))
	decimals(tx.Conn().TypeMap())
	defer tx.Rollback(db.ctx)
	block(Tx{db.ctx, tx})
}

// A Refusal is what Postgres said of a statement it refused: Kogo's
// kogo.test.Refusal, here since Tx_Refusal gives one.
type Refusal struct {
	SqlState   string
	Constraint *string // null where the refusal names none
	Message    string
}

// Tx_Refusal is tx.refusal(sql"..."), a test file's (spec.md 10.8):
// the statement runs in a savepoint, which a refusal rolls back to, so
// that the transaction goes on. It gives null when Postgres accepted
// the statement. A failure that is not Postgres's word on the
// statement, as a lost connection, is a panic.
func Tx_Refusal(this Tx, sql string, args ...any) *Refusal {
	savepoint := must(this.tx.Begin(this.ctx))
	_, err := savepoint.Exec(this.ctx, sql, bound(args)...)
	var refused *pgconn.PgError
	if !errors.As(err, &refused) || stopped(refused.Code) {
		must(0, err)
		must(0, savepoint.Commit(this.ctx))
		return nil
	}
	must(0, savepoint.Rollback(this.ctx))
	r := &Refusal{SqlState: refused.Code, Message: refused.Message}
	if refused.ConstraintName != "" {
		r.Constraint = &refused.ConstraintName
	}
	return r
}

// stopped says whether an SQLSTATE is Postgres stopping a statement
// rather than refusing it: a lost connection (08), a serialization
// failure or deadlock (40), resources (53), an operator or a timeout
// (57), a system error (58, XX). tx.refusal panics on these.
func stopped(code string) bool {
	return slices.Contains([]string{"08", "40", "53", "57", "58", "XX"}, code[:min(2, len(code))])
}

// A Scan reads the row at hand into what its arguments point to, one
// for each column.
type Scan func(dest ...any)

// rows runs a statement and reads each row of its result with read,
// which the emitter writes from the checker's facts, up to max rows
// where max is not 0.
func rows[R any](tx Tx, sql string, read func(Scan) R, args []any, max int) []R {
	result := must(tx.tx.Query(tx.ctx, sql, bound(args)...))
	defer result.Close()
	out := []R{}
	for (max == 0 || len(out) < max) && result.Next() {
		out = append(out, read(func(dest ...any) { must(0, result.Scan(dest...)) }))
	}
	must(0, result.Err())
	return out
}

// bound gives the arguments as the driver takes them. A List is never
// NULL: an empty one may be Go's nil slice, which the driver would
// send as NULL, so it is sent as an empty array.
func bound(args []any) []any {
	for i, a := range args {
		v := reflect.ValueOf(a)
		for v.Kind() == reflect.Pointer && !v.IsNil() {
			v = v.Elem()
		}
		if v.Kind() == reflect.Slice && v.IsNil() {
			args[i] = reflect.MakeSlice(v.Type(), 0, 0).Interface()
		}
	}
	return args
}

// The five calls of spec.md 10.2. R is the type the call gives for
// one row: for queryOne a T?, which is null where there is no row.
// queryOne and querySingle read no further than a second row.

func Tx_Query[R any](this Tx, sql string, read func(Scan) R, args ...any) List[R] {
	return rows(this, sql, read, args, 0)
}

func Tx_QueryOne[R any](this Tx, sql string, read func(Scan) R, args ...any) (null R) {
	switch found := rows(this, sql, read, args, 2); len(found) {
	case 0:
		return null
	case 1:
		return found[0]
	default:
		panic("queryOne read more than one row, and reads one or none: " + sql)
	}
}

func Tx_QuerySingle[R any](this Tx, sql string, read func(Scan) R, args ...any) R {
	switch found := rows(this, sql, read, args, 2); len(found) {
	case 0:
		panic("querySingle read no row, and reads exactly one: " + sql)
	case 1:
		return found[0]
	default:
		panic("querySingle read more than one row, and reads exactly one: " + sql)
	}
}

// Tx_Exists sends the statement as written and asks for its first row
// (decisions.md).
func Tx_Exists(this Tx, sql string, args ...any) bool {
	result := must(this.tx.Query(this.ctx, sql, bound(args)...))
	found := result.Next()
	result.Close()
	must(0, result.Err())
	return found
}

func Tx_Execute(this Tx, sql string, args ...any) int32 {
	return int32(must(this.tx.Exec(this.ctx, sql, bound(args)...)).RowsAffected())
}

// NotNull gives the value of a column that the row's type says is not
// null where Postgres could not: one named "name!", or one of a
// nullable nested data class that is there (spec.md 10.3, 10.5).
func NotNull[T any](p *T, column string) T {
	if p == nil {
		panic("column " + column + " is null, and fills a property that is not nullable")
	}
	return *p
}

// A Composite is where a value of a composite type is read to: Null
// is set when the value is null, and otherwise each field is read
// into what Fields holds for it, in Postgres's order.
type Composite struct {
	Null   *bool
	Fields []any
}

func (c Composite) ScanNull() error     { *c.Null = true; return nil }
func (c Composite) ScanIndex(i int) any { return c.Fields[i] }

// Row gives the data class read from a composite value, or null.
func Row[T any](null bool, v T) *T {
	if null {
		return nil
	}
	return &v
}

// Tx_Known gives this once its connection's driver knows the
// composite types named, and what their fields are made of: the driver
// reads a value of a type only when it has been told of the type, and
// is told once for each connection. citext is an extension's, so the
// driver is told of that too. A name is as Postgres writes it, quoted
// where it must be, and is found on the connection's search path; the
// driver is told of the type by its schema and its name in the
// catalog, and then by the name as written, so that it is not told
// again.
func Tx_Known(this Tx, names ...string) Tx {
	conn := this.tx.Conn()
	types := conn.TypeMap()
	known := func(name string) bool { _, ok := types.TypeForName(name); return ok }
	if names = slices.DeleteFunc(names, known); len(names) == 0 {
		return this
	}
	var citext uint32
	if !known("citext") && conn.QueryRow(this.ctx, "select oid from pg_type where typname = 'citext'").Scan(&citext) == nil {
		types.RegisterType(&pgtype.Type{Name: "citext", OID: citext, Codec: pgtype.TextCodec{}})
	}
	var found []string
	must(0, conn.QueryRow(this.ctx, `select array_agg(coalesce(n.nspname || '.' || t.typname, '') order by u.i)
		from unnest($1::text[]) with ordinality u(name, i)
		left join pg_type t on t.oid = to_regtype(u.name) and t.typtype = 'c'
		left join pg_namespace n on n.oid = t.typnamespace`, names).Scan(&found))
	if i := slices.Index(found, ""); i >= 0 {
		panic("Postgres has no composite type " + names[i] + " on this connection's search path")
	}
	must(conn.LoadTypes(this.ctx, found)) // which tells the driver of each
	for i, name := range found {
		t, _ := types.TypeForName(name)
		types.RegisterType(&pgtype.Type{Name: names[i], OID: t.OID, Codec: t.Codec})
	}
	return this
}

// A Uuid is Postgres's uuid, and a Decimal its numeric, as Postgres
// writes one: 12.50, NaN, Infinity.

func (u *Uuid) ScanUUID(v pgtype.UUID) error {
	if !v.Valid {
		return errors.New("null is not a Uuid")
	}
	*u = v.Bytes
	return nil
}

func (u Uuid) UUIDValue() (pgtype.UUID, error) { return pgtype.UUID{Bytes: u, Valid: true}, nil }

type Decimal string

func (d *Decimal) ScanNumeric(v pgtype.Numeric) error {
	if !v.Valid {
		return errors.New("null is not a Decimal")
	}
	text, err := v.Value()
	*d = Decimal(fmt.Sprint(text))
	return err
}

func (d Decimal) NumericValue() (pgtype.Numeric, error) {
	var n pgtype.Numeric
	return n, n.Scan(string(d))
}

// decimals has the driver read a numeric into a Decimal by a codec of
// its own, alone and in an array, once for each connection. The
// driver's own reads a zero, which Postgres sends with no digits, as
// 0 whatever its scale; this one reads 0.00 for a zero of scale 2.
func decimals(types *pgtype.Map) {
	if t, ok := types.TypeForOID(pgtype.NumericOID); ok {
		if _, ours := t.Codec.(numericCodec); ours {
			return
		}
	}
	numeric := &pgtype.Type{Name: "numeric", OID: pgtype.NumericOID, Codec: numericCodec{}}
	types.RegisterType(numeric)
	types.RegisterType(&pgtype.Type{Name: "_numeric", OID: pgtype.NumericArrayOID, Codec: &pgtype.ArrayCodec{ElementType: numeric}})
}

type numericCodec struct{ pgtype.NumericCodec }

func (c numericCodec) PlanScan(m *pgtype.Map, oid uint32, format int16, target any) pgtype.ScanPlan {
	plan := c.NumericCodec.PlanScan(m, oid, format, target)
	if _, ok := target.(*Decimal); ok && format == pgtype.BinaryFormatCode {
		return zeroScan{plan}
	}
	return plan
}

// A zeroScan reads a numeric into a Decimal, a zero by its header:
// no digits, a sign of 0 and the scale.
type zeroScan struct{ pgtype.ScanPlan }

func (p zeroScan) Scan(src []byte, dst any) error {
	if len(src) == 8 && binary.BigEndian.Uint16(src) == 0 && binary.BigEndian.Uint16(src[4:]) == 0 {
		text := "0"
		if scale := int(binary.BigEndian.Uint16(src[6:])); scale > 0 {
			text += "." + strings.Repeat("0", scale)
		}
		*dst.(*Decimal) = Decimal(text)
		return nil
	}
	return p.ScanPlan.Scan(src, dst)
}
