package sqlpass

import (
	"context"
	"strconv"
	"strings"
)

// A node is one value of the text Postgres prints for a parse tree:
// {TAG :field value ...}, a (list ...), or a bare atom. Every accessor
// accepts nil, so a tree that is not as expected reads as "nullable".
type node struct {
	tag    string
	fields map[string]*node
	list   []*node
	atom   string
}

type reader struct {
	toks []string
	i    int
}

func parseTree(s string) *node {
	r := &reader{}
	for i := 0; i < len(s); {
		switch c := s[i]; {
		case c == ' ' || c == '\n' || c == '\t':
			i++
		case strings.IndexByte("(){}", c) >= 0:
			r.toks = append(r.toks, s[i:i+1])
			i++
		default:
			j := i
			for j < len(s) && strings.IndexByte(" \n\t(){}", s[j]) < 0 {
				if s[j] == '\\' {
					j++
				}
				j++
			}
			j = min(j, len(s))
			r.toks = append(r.toks, s[i:j])
			i = j
		}
	}
	return r.value()
}

func (r *reader) peek() string {
	if r.i < len(r.toks) {
		return r.toks[r.i]
	}
	return ""
}

func (r *reader) value() *node {
	t := r.peek()
	r.i++
	switch t {
	case "{":
		n := &node{tag: r.peek(), fields: map[string]*node{}}
		r.i++
		for p := r.peek(); p != "}" && p != ""; p = r.peek() {
			r.i++ // p is ":name"; its value is what follows, up to the next name
			var first *node
			for q := r.peek(); q != "}" && q != "" && q[0] != ':'; q = r.peek() {
				if v := r.value(); first == nil {
					first = v
				}
			}
			n.fields[p[1:]] = first
		}
		r.i++
		return n
	case "(":
		n := &node{}
		for p := r.peek(); p != ")" && p != ""; p = r.peek() {
			n.list = append(n.list, r.value())
		}
		r.i++
		return n
	}
	return &node{atom: t}
}

// params records where each parameter the tree refers to lies, as a
// byte offset in the statement's text. Kind 0 is a parameter of the
// statement; Postgres numbers its own, as for a row compared with a
// subquery, from 1 as well.
func (n *node) params(at map[int]bool) {
	if n == nil {
		return
	}
	if n.tag == "PARAM" && n.num("paramkind") == 0 {
		at[n.num("location")] = true
	}
	for _, f := range n.fields {
		f.params(at)
	}
	for _, v := range n.list {
		v.params(at)
	}
}

// Postgres's bits for what a statement needs of a table
// (requiredPerms), and one of this package's for a row lock.
var needs = []struct {
	bit  int
	name string
}{{1, "insert"}, {4, "update"}, {8, "delete"}, {1 << 30, "lock"}}

// perms records what the tree says the statement needs of each table
// or view it lists, by oid, in a subquery or a with too: the bits of
// each RTEPERMISSIONINFO. Postgres asks update's of a table whose rows
// the statement locks and names no column updated, which is told
// apart here as a lock.
func (n *node) perms(seen map[int]int) {
	if n == nil {
		return
	}
	if n.tag == "RTEPERMISSIONINFO" {
		p := n.num("requiredPerms")
		if p&4 != 0 && len(n.field("updatedCols").items()) <= 1 { // (b) is no column
			p = p&^4 | 1<<30
		}
		seen[n.num("relid")] |= p
	}
	for _, f := range n.fields {
		f.perms(seen)
	}
	for _, v := range n.list {
		v.perms(seen)
	}
}

func (n *node) field(name string) *node {
	if n == nil {
		return nil
	}
	return n.fields[name]
}

func (n *node) num(name string) int {
	i, err := strconv.Atoi(n.field(name).text())
	if err != nil {
		return -1
	}
	return i
}

func (n *node) text() string {
	if n == nil {
		return ""
	}
	return n.atom
}

func (n *node) items() []*node {
	if n == nil {
		return nil
	}
	return n.list
}

// Postgres's numbering of statement and range table kinds.
const (
	cmdSelect   = 1
	cmdDelete   = 4 // update and insert lie between; merge is 5
	rteRelation = 0
	rteSubquery = 1
	rteGroup    = 9
)

// outputs gives the expressions of a query's result columns, or nil
// when they are not plain: a set operation takes each column from
// several queries, and MERGE can null either side.
func outputs(q *node) []*node {
	var entries, exprs []*node
	switch c := q.num("commandType"); {
	case c == cmdSelect && q.field("setOperations").text() == "<>":
		entries = q.field("targetList").items()
	case c > cmdSelect && c <= cmdDelete:
		entries = q.field("returningList").items()
	}
	for _, e := range entries {
		if e.field("resjunk").text() == "false" {
			exprs = append(exprs, e.field("expr"))
		}
	}
	return exprs
}

// jsonbCasts are pg_catalog's casts from jsonb, by oid: each gives
// null for JSON's null.
var jsonbCasts = map[int]bool{3556: true, 3449: true, 3450: true, 3451: true, 3452: true, 3453: true, 2580: true}

// notNull reports whether expr, an expression of query q, cannot be
// null by spec 10.5, and whether it is a table's whole row at all,
// nulled or not. Nothing nulls a reference to a NOT NULL column or a
// table's whole row; count; exists; a coalesce with an argument that
// is not null, a constant included; or a cast of any of these, but
// one from jsonb or by a function of the schema's.
// constant is whether a constant that is not null counts, as it does
// among coalesce's arguments and nowhere else.
func (c *Checker) notNull(ctx context.Context, q, expr *node, constant bool) (notNull, whole bool, err error) {
	q, expr, rte, nulled := source(q, expr)
	whole = rte != nil && expr.num("varattno") == 0
	switch {
	case nulled || expr == nil:
	case rte != nil && whole:
		notNull = true
	case rte != nil:
		notNull, err = c.columnNotNull(ctx, rte.num("relid"), expr.num("varattno"), rte.field("inh").text() != "false")
	case expr.tag == "AGGREF": // count() and count("any"), pg_catalog's by oid
		notNull = expr.num("aggfnoid") == 2803 || expr.num("aggfnoid") == 2147
	case expr.tag == "SUBLINK":
		notNull = expr.num("subLinkType") == 0 // EXISTS_SUBLINK
	case expr.tag == "CONST":
		notNull = constant && expr.field("constisnull").text() == "false"
	case expr.tag == "COALESCEEXPR":
		for _, arg := range expr.field("args").items() {
			if notNull, _, err = c.notNull(ctx, q, arg, true); notNull || err != nil {
				break
			}
		}
	case expr.tag == "RELABELTYPE" || expr.tag == "COERCEVIAIO":
		notNull, _, err = c.notNull(ctx, q, expr.field("arg"), constant)
	case expr.tag == "FUNCEXPR" && (expr.num("funcformat") == 1 || expr.num("funcformat") == 2) &&
		expr.num("funcid") < 16384 && !jsonbCasts[expr.num("funcid")]:
		// a cast function of Postgres's own (below FirstNormalObjectId),
		// explicit or implicit: its first argument is the value, any
		// others a type modifier
		if args := expr.field("args").items(); len(args) > 0 {
			notNull, _, err = c.notNull(ctx, q, args[0], constant)
		}
	}
	return notNull, whole, err
}

// source follows expr, an expression of query q, through references
// to a subquery's column or a grouped column, to what they name: an
// expression of the query it ends in, and when that is a reference to
// a table, the table's range table entry. nulled is whether an outer
// join, a grouping set or returning's old or new can null a reference
// on the way: Postgres marks a reference with the outer joins and
// grouping sets that null it (varnullingrels).
func source(q, expr *node) (endQ, endExpr, rte *node, nulled bool) {
	for expr != nil && expr.tag == "VAR" && expr.num("varlevelsup") == 0 {
		nulled = nulled || expr.num("varreturningtype") != 0 || len(expr.field("varnullingrels").items()) != 1
		rtable, att, no := q.field("rtable").items(), expr.num("varattno"), expr.num("varno")
		if no < 1 || no > len(rtable) || att < 0 {
			break
		}
		in, exprs := q, []*node(nil)
		switch rte := rtable[no-1]; rte.num("rtekind") {
		case rteRelation:
			return q, expr, rte, nulled
		case rteSubquery:
			in = rte.field("subquery")
			exprs = outputs(in)
		case rteGroup:
			exprs = rte.field("groupexprs").items()
		}
		if att < 1 || att > len(exprs) {
			break
		}
		q, expr = in, exprs[att-1]
	}
	return q, expr, nil, nulled
}
