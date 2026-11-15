package kogo

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"

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
// At SERIALIZABLE, block is run again from the start for as long as
// Postgres ends it with a serialization failure or a deadlock.
func Db_Tx_Boolean_TxToA[A any](this Db, serializable *bool, block func(Tx) A) A {
	opts := pgx.TxOptions{}
	if serializable != nil && *serializable {
		opts.IsoLevel = pgx.Serializable
	}
	for {
		if a, done := attempt(this, opts, block); done {
			return a
		}
	}
}

// attempt runs block in one transaction. It is not done when the
// transaction is to be run again.
func attempt[A any](db Db, opts pgx.TxOptions, block func(Tx) A) (a A, done bool) {
	tx := must(db.pool.BeginTx(db.ctx, opts))
	defer func() {
		if done {
			return
		}
		p := recover()
		tx.Rollback(db.ctx)
		var refused *pgconn.PgError
		if err, ok := p.(error); !ok || opts.IsoLevel != pgx.Serializable || !errors.As(err, &refused) ||
			refused.Code != "40001" && refused.Code != "40P01" {
			panic(p)
		}
	}()
	a = block(Tx{db.ctx, tx})
	must(0, tx.Commit(db.ctx))
	return a, true
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

// A Scan reads the row at hand into what its arguments point to, one
// for each column.
type Scan func(dest ...any)

// rows runs a statement and reads each row of its result with read,
// which the emitter writes from the checker's facts.
func rows[R any](tx Tx, sql string, read func(Scan) R, args []any) []R {
	result := must(tx.tx.Query(tx.ctx, sql, bound(args)...))
	defer result.Close()
	out := []R{}
	for result.Next() {
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

func Tx_Query[R any](this Tx, sql string, read func(Scan) R, args ...any) List[R] {
	return rows(this, sql, read, args)
}

func Tx_QueryOne[R any](this Tx, sql string, read func(Scan) R, args ...any) (null R) {
	switch found := rows(this, sql, read, args); len(found) {
	case 0:
		return null
	case 1:
		return found[0]
	default:
		panic(fmt.Sprintf("queryOne read %d rows, and reads one or none: %s", len(found), sql))
	}
}

func Tx_QuerySingle[R any](this Tx, sql string, read func(Scan) R, args ...any) R {
	found := rows(this, sql, read, args)
	if len(found) != 1 {
		panic(fmt.Sprintf("querySingle read %d rows, and reads exactly one: %s", len(found), sql))
	}
	return found[0]
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
// driver is told of that too.
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
	must(conn.LoadTypes(this.ctx, names)) // which tells the driver of each
	if i := slices.IndexFunc(names, func(name string) bool { return !known(name) }); i >= 0 {
		panic("Postgres has no composite type " + names[i] + " on this connection's search path")
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
