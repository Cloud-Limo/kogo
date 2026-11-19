package check

import (
	"github.com/Cloud-Limo/kogo/syntax"
)

// A gate reads and decides; it does not write (spec.md 15.3;
// doc/gates.md). The server holds a gate on a SerializableTx to it
// when it runs; this holds every gate to it when it compiles, by what
// Postgres's tree says each statement the gate reaches needs of each
// table (sqlpass.Table), which is not re-read here.

// A reached is a sql literal and the file it is written in.
type reached struct {
	lit  *syntax.SQL
	file *syntax.File
}

// gateWrites refuses each statement that a gate reaches, in its own
// body or through the funs and vals it refers to, and that writes a
// table, or locks one where the gate's transaction is a
// SerializableTx, or is a utility statement, as truncate or lock
// table, whose tree lists no table. A statement is refused once, for the first gate that
// reaches it.
func (c *checker) gateWrites(program []*syntax.File) {
	said := map[*syntax.SQL]bool{}
	for _, f := range program {
		for _, d := range f.APIs {
			for _, op := range c.info.APIs[d].Ops {
				if op.Gate == nil {
					continue
				}
				seen := map[Object]bool{}
				var walk func(Object)
				walk = func(o Object) {
					if seen[o] {
						return
					}
					seen[o] = true
					for _, s := range c.stmts[o] {
						if q := c.info.SQL[s.lit]; q.Stmt != nil && !said[s.lit] {
							c.file = s.file
							said[s.lit] = c.strays(s.lit, q, op)
						}
					}
					for _, r := range c.reads[o] {
						walk(r)
					}
				}
				walk(op.Gate)
			}
		}
	}
}

// strays reports whether the statement of lit does what op's gate
// must not, and says so of the first table it does it to.
func (c *checker) strays(lit *syntax.SQL, q *Query, op *Op) bool {
	if q.Stmt.Utility != "" {
		c.errorf(lit, "gate-writes", "this statement is a utility statement (Postgres's %s), and a gate only reads: move it to the work, and bind a setting with set_config (the gate %s reaches it; spec.md 15.3)", q.Stmt.Utility, op.Gate.Name)
		return true
	}
	does := map[string]string{"insert": "inserts into", "update": "updates", "delete": "deletes from"}
	for _, t := range q.Stmt.Tables {
		for _, need := range t.Needs {
			switch {
			case need == "lock" && op.Serializable:
				c.errorf(lit, "gate-writes", "this statement locks %s, and a gate on a SerializableTx only reads: lock it in the work (the gate %s reaches it; spec.md 15.3)", t.Name, op.Gate.Name)
				return true
			case need != "lock":
				c.errorf(lit, "gate-writes", "this statement %s %s, and a gate only reads: move it to the work (the gate %s reaches it; spec.md 15.3)", does[need], t.Name, op.Gate.Name)
				return true
			}
		}
	}
	return false
}
