// Package check checks a Kogo program against spec.md sections 2 to
// 11: names, types, type arguments, overloads, narrowing, when,
// expected failures, SQL and markers. It reads the parser's trees, the
// prelude's among them, and the SQL pass's signatures, and reports
// errors or gives the facts the emitter needs.
package check

import "kogo/syntax"

// Info is what the checker learned about a program: tables keyed by
// the nodes of the parser's trees.
type Info struct {
	// Types holds the type of every expression that is a value, as it
	// is read there (a narrowed name has its narrowed type), and of
	// every written type.
	Types map[syntax.Node]Type
	// Uses holds what each *syntax.Ident, *syntax.Select,
	// *syntax.FuncRef and *syntax.ClassRef refers to. A name that refers to a property or
	// to a function with a receiver, and is not a Select, is read on
	// this.
	Uses map[syntax.Node]Object
	// Calls holds the function behind each *syntax.Call, behind each
	// name that reads an extension val, and behind each operator that
	// stands for a binding: a + b, a < b, a in b, a to b, a[i], and
	// the same in a += b and in a when's in condition.
	Calls map[syntax.Node]*Call
	// Narrowed holds each use of a name that is narrowed there, and
	// the type it is narrowed to; its Var holds the declared type.
	Narrowed map[*syntax.Ident]Type
	// Vars holds what a *syntax.ValDecl, *syntax.Param,
	// *syntax.LambdaParam and *syntax.For declare: one Var, or one
	// for each name of a destructuring. A *syntax.Lambda that names no
	// parameter and takes one declares it.
	Vars map[syntax.Node][]*Var
	// Funcs holds the function of each *syntax.FunDecl and of each
	// *syntax.ValDecl that is an extension val.
	Funcs map[syntax.Node]*Func
	// Classes holds the type of each *syntax.ClassDecl.
	Classes map[*syntax.ClassDecl]*TypeDecl
	// Fails holds the error type E of each function, lambda and call
	// that can fail (spec.md section 9). A *syntax.FunDecl whose
	// function has the receiver Raise<E> or returns Either<E, A>,
	// whichever spelling is used, and a *syntax.ValDecl, an extension
	// val, that returns Either<E, A>. A *syntax.Lambda that is a raise
	// scope for E, the block of either or withError, or that holds a
	// call failing into the raise scope around it, which fails with
	// E: running it can fail. A lambda that returns an Either is not
	// one; its type says so. A *syntax.Call that makes the raise scope
	// it is in fail: bind, and a call of a fun whose receiver is
	// Raise<E>, raise, ensure, ensureNotNull and withError among them.
	// A call that gives an Either value is not one: bind on its value
	// is.
	Fails map[syntax.Node]Type
	// SQL holds each sql literal that one of the five calls of
	// spec.md 10.2 takes, as matched with its row type and arguments.
	SQL map[*syntax.SQL]*Query
}

// An Object is what a name refers to: a *Var, a *Func or a *TypeDecl.
type Object interface{ object() }

func (*Var) object()      {}
func (*Func) object()     {}
func (*TypeDecl) object() {}

// A Call is a call as the checker settled it.
type Call struct {
	Func     *Func           // nil when a value of a function type is called
	TypeArgs []Type          // one for each of Func.TypeParams
	Receiver syntax.Expr     // nil when Func has none, or when it is this or the raise scope, unwritten
	Args     [][]syntax.Expr // for each parameter, what fills it: nothing where its default does, several for a vararg
}

type VarKind int

const (
	Local    VarKind = iota // a val or a var in a body, a for's name, a lambda's parameter, it
	Param                   // a function's parameter, and this
	Property                // of a data class or an interface; an enum's name and entries
	Global                  // a top-level val
	Entry                   // an enum's constant
)

// A Var is a name that holds a value.
type Var struct {
	Kind    VarKind
	Name    string
	Type    Type
	Mutable bool        // a var
	Owner   *TypeDecl   // of a Property or an Entry
	Decl    syntax.Node // nil for an enum's name and entries, and for this
	vis
}

type FuncKind int

const (
	Fun         FuncKind = iota // a fun: top-level, local, an extension, an override, or an external object's
	Getter                      // an extension val
	Method                      // an interface's fun
	Constructor                 // of a data class
	Copy                        // a data class's copy
	ToString                    // a data class's toString
)

// A Func is anything that is called. A Method, an override, a Copy
// and a ToString have their owner as the receiver, and its type
// parameters before their own.
type Func struct {
	Kind       FuncKind
	Name       string
	TypeParams []*TypeParam
	Receiver   Type // nil when there is none
	Params     []*Var
	Result     Type
	External   bool        // a binding: Go provides it
	Owner      *TypeDecl   // of a Method, a Constructor, a Copy, a ToString, an override and an object's fun
	Decl       syntax.Node // a *syntax.FunDecl, a *syntax.ValDecl or a *syntax.ClassDecl; nil for Copy and ToString
	vis
	owners int // how many of TypeParams are the owner's
}

// A TypeDecl is a declared type.
type TypeDecl struct {
	Kind     string // as syntax.ClassDecl's, or "builtin": Boolean, Int, Long, Double, String, Char, Unit, Nothing
	Name     string // as its package knows it: Outcome.Ok
	Params   []*TypeParam
	Supers   []*Named    // the interfaces a data class or a data object implements
	Props    []*Var      // a data class's properties in order; an interface's vals
	Methods  []*Func     // an interface's funs, a data class's overrides, an external object's functions
	Ctor     *Func       // of a data class; its parameters are the properties
	Variants []*TypeDecl // of a sealed interface, in source order
	Entries  []*Var      // of an enum
	Decl     *syntax.ClassDecl
	vis
	copy, toString *Func // made when first called
}

// vis is where a declaration lives, who may name it, and the markers
// that a file must opt in to before it uses it (spec.md 11).
type vis struct {
	Pkg        string
	file       *syntax.File
	visibility string // "", "private" or "internal"
	markers    []*TypeDecl
}
