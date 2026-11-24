package kogo

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"time"

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

// CheckPool panics, naming the setting, where pool is not as Kogo
// requires of the pool an application makes and hands over (spec.md
// 15.5, the database handle): every connection in UTC, and each
// closed past a longest life. The handler and the test runner check
// it at start.
func CheckPool(pool *pgxpool.Pool) {
	c := pool.Config()
	utc := false // Postgres reads the key in any case, pgx keeps it as written
	for k, v := range c.ConnConfig.RuntimeParams {
		if strings.EqualFold(k, "timezone") {
			utc = strings.EqualFold(v, "UTC")
			if !utc {
				break
			}
		}
	}
	if !utc {
		panic(`the pool does not set timezone to UTC, which Kogo requires: set ConnConfig.RuntimeParams["timezone"] = "UTC", and no other key that is timezone in another case (spec.md 15.5)`)
	} else if c.MaxConnLifetime <= 0 {
		panic("the pool's MaxConnLifetime is not above 0, so a connection lives for ever: leave pgxpool's hour or set another (spec.md 15.5)")
	}
}

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
// It is at READ COMMITTED, and run once; db.tx(serializable = true)
// is at SERIALIZABLE, and rerun on each conflict.
func Db_Tx_Boolean_TxToA[A any](this Db, serializable *bool, block func(Tx) A) A {
	s := serializable != nil && *serializable
	return InTx(this, s, func(*pgconn.PgError) bool { return s }, block)
}

// InTx is db.tx { }, for the server too, which limits its reruns: at
// SERIALIZABLE where serializable says so, and at READ COMMITTED
// where not, as stated and not left to the database's default. block
// is run again from the start for as long as Postgres ends it with a
// serialization failure or a deadlock and again, told which, says to
// go on, after a random pause, up to 5 ms after the first conflict
// and 5 ms more after each, to 50, so that the two that conflicted
// do not meet again in step; db's context ends the pause. Where again
// says not to, Postgres's error is a panic.
func InTx[A any](db Db, serializable bool, again func(conflict *pgconn.PgError) bool, block func(Tx) A) A {
	level := pgx.ReadCommitted
	if serializable {
		level = pgx.Serializable
	}
	for n := 1; ; n++ {
		a, conflict := attempt(db, pgx.TxOptions{IsoLevel: level}, block)
		if conflict == nil {
			return a
		}
		if !again(conflict) {
			panic(conflict)
		}
		pause := time.NewTimer(rand.N(time.Duration(min(n, 10)) * 5 * time.Millisecond))
		select {
		case <-pause.C:
		case <-db.ctx.Done():
			pause.Stop()
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
		if err, ok := p.(error); !ok || !errors.As(err, &conflict) ||
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

// RolledBack runs block once in a transaction at SERIALIZABLE, as
// stated and not left to the database's default, that is rolled back
// when block ends, however it ends: kogo test's, one for each test,
// whose block is handed a SerializableTx (spec.md 16.1).
func RolledBack(db Db, block func(Tx)) {
	tx := must(db.pool.BeginTx(db.ctx, pgx.TxOptions{IsoLevel: pgx.Serializable}))
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
// send as NULL, so it is sent as an empty array. A String that holds
// U+0000, alone or in a List, is a panic that says so: Postgres's
// text holds none (spec.md 10.4).
func bound(args []any) []any {
	for i, a := range args {
		v := reflect.ValueOf(a)
		for v.Kind() == reflect.Pointer && !v.IsNil() {
			v = v.Elem()
		}
		if v.Kind() == reflect.Slice && v.IsNil() {
			args[i] = reflect.MakeSlice(v.Type(), 0, 0).Interface()
		}
		noZero(v)
		if v.Kind() != reflect.Slice {
			continue
		}
		if e := v.Type().Elem(); e.Kind() == reflect.String || e.Kind() == reflect.Pointer && e.Elem().Kind() == reflect.String {
			for j := range v.Len() {
				noZero(reflect.Indirect(v.Index(j)))
			}
		}
	}
	return args
}

func noZero(v reflect.Value) {
	if v.Kind() == reflect.String && strings.ContainsRune(v.String(), 0) {
		panic("a String that holds U+0000 cannot be sent to Postgres, whose text holds none (spec.md 10.4)")
	}
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
	return affected(must(this.tx.Exec(this.ctx, sql, bound(args)...)).RowsAffected())
}

// affected gives execute's rows as its Int. Postgres counts in 64 bits:
// a count past an Int is a panic, never the Int its low bits make.
func affected(rows int64) int32 {
	if rows > math.MaxInt32 {
		panic(fmt.Sprintf("execute changed %d rows, more than the Int it gives can hold (spec.md 10.4)", rows))
	}
	return int32(rows)
}

// An Array is where an array is read to: each element as a T?, since
// Postgres says of no array that its elements are not null. Kogo
// reads one dimension (spec.md 10.4): an array of more is a panic,
// never the flat List the driver would make of it.
type Array[T any] struct{ elems *List[*T] }

func (a *Array[T]) SetDimensions(d []pgtype.ArrayDimension) error {
	a.elems = nil
	if len(d) > 1 {
		return fmt.Errorf("an array of %d dimensions is not a List, which has one (spec.md 10.4)", len(d))
	}
	if d != nil {
		list := List[*T]{}
		if len(d) == 1 {
			list = make(List[*T], d[0].Length)
		}
		a.elems = &list
	}
	return nil
}

func (a *Array[T]) ScanIndex(i int) any { return &(*a.elems)[i] }
func (a *Array[T]) ScanIndexType() any  { return new(*T) }

// Nullable gives what was read as a List<T?>, or null for a NULL.
func (a *Array[T]) Nullable() *List[*T] { return a.elems }

// List gives what was read as a List<T>, or null for a NULL. A NULL
// element is a panic that names the column.
func (a *Array[T]) List(column string) *List[T] {
	if a.elems == nil {
		return nil
	}
	list := make(List[T], len(*a.elems))
	for i, e := range *a.elems {
		if e == nil {
			panic(fmt.Sprintf("column %s holds an array whose element %d is NULL, and fills a List whose elements are not nullable (spec.md 10.4): read it as a List<T?>, or keep the nulls out in the statement", column, i))
		}
		list[i] = *e
	}
	return &list
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

// Tx_Known gives this once its connection's driver knows the types
// named, arrays that Postgres does not ship (one of a domain, a domain
// over one), and what each is made of: the driver reads a value of a
// type only when it has been told of the type, and is told once for
// each connection. citext is an extension's, so the driver is told of
// that too. A name is as Postgres writes it, quoted where it must be,
// and is found on the connection's search path; the driver is told of
// the type by its schema and its name in the catalog, and then by the
// name as written, so that it is not told again.
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
		left join pg_type t on t.oid = to_regtype(u.name)
		left join pg_namespace n on n.oid = t.typnamespace`, names).Scan(&found))
	if i := slices.Index(found, ""); i >= 0 {
		panic("Postgres has no type " + names[i] + " on this connection's search path")
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

// A ByteArray is Postgres's bytea: bytes that Kogo carries and never
// reads or changes (spec.md 3). It is read into memory of its own,
// since the driver's is lent, and a null is refused where a ByteArray
// is wanted. One of no bytes is sent as that, never as NULL (bound).
type ByteArray []byte

func (b *ByteArray) ScanBytes(v []byte) error {
	if v == nil {
		return errors.New("null is not a ByteArray")
	}
	*b = append(ByteArray{}, v...)
	return nil
}

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
