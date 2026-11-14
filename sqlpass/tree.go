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

// params records the number of each parameter the tree refers to.
// Kind 0 is a parameter of the statement; Postgres numbers its own,
// as for a row compared with a subquery, from 1 as well.
func (n *node) params(seen map[int]bool) {
	if n == nil {
		return
	}
	if n.tag == "PARAM" && n.num("paramkind") == 0 {
		seen[n.num("paramid")] = true
	}
	for _, f := range n.fields {
		f.params(seen)
	}
	for _, v := range n.list {
		v.params(seen)
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

// notNull reports whether expr, an expression of query q, is a plain
// reference to a NOT NULL column that nothing in the query can null.
// Postgres marks a reference with the outer joins and grouping sets
// that null it (varnullingrels); a reference to a subquery's column
// or a grouped column is followed to what it names.
func (c *Checker) notNull(ctx context.Context, q, expr *node) (bool, error) {
	if expr == nil || expr.tag != "VAR" || expr.num("varlevelsup") != 0 ||
		expr.num("varreturningtype") != 0 || len(expr.field("varnullingrels").items()) != 1 {
		return false, nil
	}
	rtable, att := q.field("rtable").items(), expr.num("varattno")
	if no := expr.num("varno"); no >= 1 && no <= len(rtable) && att >= 1 {
		switch rte := rtable[no-1]; rte.num("rtekind") {
		case rteRelation:
			return c.columnNotNull(ctx, rte.num("relid"), att)
		case rteSubquery:
			if sub := rte.field("subquery"); att <= len(outputs(sub)) {
				return c.notNull(ctx, sub, outputs(sub)[att-1])
			}
		case rteGroup:
			if exprs := rte.field("groupexprs").items(); att <= len(exprs) {
				return c.notNull(ctx, q, exprs[att-1])
			}
		}
	}
	return false, nil
}
