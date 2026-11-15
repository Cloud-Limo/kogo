// Package syntax reads Kogo source files into syntax trees: spec.md
// sections 2 to 13. It resolves no names and checks no types.
package syntax

// A Pos is a place in a source file: a byte offset, and a line and a
// column counted from 1, the column in characters.
type Pos struct{ Offset, Line, Column int }

// At gives the position of a node: where its first token starts, or
// for an operator or a member, the operator or the member's name.
func (p Pos) At() Pos { return p }

// A Node is any part of the tree.
type Node interface{ At() Pos }

// A Stmt is an Expr, a *ValDecl, a *FunDecl, an *Assign, a *For or a
// *While.
type Stmt = Node

// A Decl is a *FunDecl, a *ValDecl, a *ClassDecl or a *TypeAlias.
type Decl interface {
	Node
	decl()
}

// An Expr is a value.
type Expr interface {
	Node
	expr()
}

// A Type is a *NamedType or a *FuncType.
type Type interface {
	Node
	typ()
}

// A File is one source file.
type File struct {
	Name    string
	OptIn   []*Annotation // the @file:OptIn(...) lines
	Package *Ident        // a dotted name, as cusoon.model
	Imports []*Ident
	Decls   []Decl
	SQL     []*SQL // every sql literal of the file, in source order
}

// An Annotation is @Name or @Name(args), and in a file's head,
// @file:OptIn(X::class, ...).
type Annotation struct {
	Pos
	Name string
	Args []Expr
}

// Modifiers are what may precede a declaration or a constructor.
type Modifiers struct {
	Annotations []*Annotation
	Visibility  string // "", "private" or "internal"
	Override    bool
	Const       bool
	External    bool // a binding: spec.md section 13
}

// A FunDecl is a function. A body is a block, an = expression, or in
// an interface or an external object, absent.
type FunDecl struct {
	Pos
	Mods       Modifiers
	TypeParams []*TypeParam
	Receiver   Type
	Name       string
	Params     []*Param
	Result     Type
	Body       *Block
	Expr       Expr
}

// A Param is a parameter of a function or a data class's constructor;
// Val is set for the latter. Only a binding's may be Vararg.
type Param struct {
	Pos
	Override bool
	Val      bool
	Vararg   bool
	Name     string
	Type     Type
	Default  Expr
}

// A TypeParam is a type parameter; Out marks out variance.
type TypeParam struct {
	Pos
	Out  bool
	Name string
}

// A ValDecl is a val or a var. One of the form val (a, b) = x has
// Destructure set and no Name. An extension val has a Receiver and a
// Getter, the expression of its get() = ...; a binding's has neither.
type ValDecl struct {
	Pos
	Mods        Modifiers
	Var         bool
	TypeParams  []*TypeParam
	Receiver    Type
	Name        string
	Destructure []string
	Type        Type
	Value       Expr
	Getter      Expr
}

// A ClassDecl is a type with a body: Kind is "data class", "data
// object", "interface", "sealed interface", "enum class", "annotation
// class", "external class" or "external object".
type ClassDecl struct {
	Pos
	Mods       Modifiers
	Kind       string
	Name       string
	TypeParams []*TypeParam
	Ctor       Modifiers // of the word constructor, when written
	Params     []*Param
	Supers     []Type
	Body       []Decl
	Entries    []*Ident // of an enum
}

// A TypeAlias is typealias Name<T> = Type.
type TypeAlias struct {
	Pos
	Mods       Modifiers
	Name       string
	TypeParams []*TypeParam
	Type       Type
}

// A NamedType is a type by name, as Outcome.Ok, List<T> or Int?.
type NamedType struct {
	Pos
	Name     string
	Args     []Type
	Nullable bool
}

// A FuncType is (A, B) -> R, or with a receiver, Raise<E>.() -> R.
type FuncType struct {
	Pos
	Receiver Type
	Params   []Type
	Result   Type
	Nullable bool
}

// A Block is a list of statements. A body written without braces, as
// the then of if (c) x, is a block of one statement.
type Block struct {
	Pos
	Stmts []Stmt
}

// An Assign is target = value, or with Op "+=" and the like.
type Assign struct {
	Pos
	Op     string
	Target Expr
	Value  Expr
}

// A For is for (x in iter) or for ((a, b) in iter).
type For struct {
	Pos
	Name        string
	Destructure []string
	Iter        Expr
	Body        *Block
}

// A While is while (cond) body.
type While struct {
	Pos
	Cond Expr
	Body *Block
}

// An Ident is a name, or this.
type Ident struct {
	Pos
	Name string
}

// A Literal is a constant. Kind is "Int", "Long", "Double", "Char",
// "Boolean", "Null" or "String". Value is the text with underscores
// and an L removed, or a character or a string piece with its escapes
// read.
type Literal struct {
	Pos
	Kind  string
	Value string
}

// A StringLit is "..." or """...""": its pieces are *Literal of kind
// String and the expressions of $name and ${expr}.
type StringLit struct {
	Pos
	Raw   bool
	Parts []Expr
}

// A SQL is a sql"..." or sql"""...""" literal, the argument of the
// call named Call (query, ..., execute). Text is what lies between
// the quotes, starting at TextPos; Params are its $name and ${expr}
// in order, each at its $.
type SQL struct {
	Pos
	Call    string
	Text    string
	TextPos Pos
	Params  []*SQLParam
}

// Typed reports whether the call uses the result's columns, so that
// they must be typed: query, queryOne and querySingle do; exists and
// execute discard them.
func (s *SQL) Typed() bool { return sqlCalls[s.Call] }

// A SQLParam is $name, with Name set, or ${expr}, with Expr set.
type SQLParam struct {
	Pos
	Name string
	Expr Expr
}

// A Unary is -x or !x.
type Unary struct {
	Pos
	Op string
	X  Expr
}

// A Binary is x op y, positioned at op. Op is an arithmetic,
// comparison, equality or logical operator, "..", "until", "to", "in"
// or "?:".
type Binary struct {
	Pos
	Op string
	X  Expr
	Y  Expr
}

// An Is is x is Type, positioned at is.
type Is struct {
	Pos
	X    Expr
	Type Type
}

// A Call is f(args), f<T>(args) or f { }, positioned at its first
// bracket. A trailing lambda is its last argument.
type Call struct {
	Pos
	Fun      Expr
	TypeArgs []Type
	Args     []*Arg
}

// An Arg is an argument, named or not. Trailing marks a lambda written
// after the parentheses, which fills the last parameter.
type Arg struct {
	Pos
	Name     string
	Value    Expr
	Trailing bool
}

// A Select is x.name or x?.name, positioned at name.
type Select struct {
	Pos
	X    Expr
	Safe bool
	Name string
}

// An Index is x[i].
type Index struct {
	Pos
	X     Expr
	Index Expr
}

// A Lambda is { params -> body } or { body }.
type Lambda struct {
	Pos
	Params []*LambdaParam
	Body   *Block
}

// A LambdaParam is a name, a name with a type, or (a, b).
type LambdaParam struct {
	Pos
	Name        string
	Destructure []string
	Type        Type
}

// An If is if (cond) then else else; Else may be nil.
type If struct {
	Pos
	Cond Expr
	Then *Block
	Else *Block
}

// A When is when (subject) { branches } or, with no Subject,
// when { branches }.
type When struct {
	Pos
	Subject  Expr
	Branches []*Branch
}

// A Branch of a when; an else branch has no Conds.
type Branch struct {
	Pos
	Conds []*Cond
	Body  *Block
}

// A Cond is one condition of a branch: Op "" with X, a value or with
// no subject a Boolean; "in" with X; "is" with Type.
type Cond struct {
	Pos
	Op   string
	X    Expr
	Type Type
}

// A FuncRef is ::name.
type FuncRef struct {
	Pos
	Name string
}

// A ClassRef is X::class, found only in @file:OptIn(...).
type ClassRef struct {
	Pos
	Name string
}

// A Jump is return, return value, break or continue.
type Jump struct {
	Pos
	Op    string
	Value Expr
}

func (*FunDecl) decl()   {}
func (*ValDecl) decl()   {}
func (*ClassDecl) decl() {}
func (*TypeAlias) decl() {}

func (*NamedType) typ() {}
func (*FuncType) typ()  {}

func (*Ident) expr()     {}
func (*Literal) expr()   {}
func (*StringLit) expr() {}
func (*SQL) expr()       {}
func (*Unary) expr()     {}
func (*Binary) expr()    {}
func (*Is) expr()        {}
func (*Call) expr()      {}
func (*Select) expr()    {}
func (*Index) expr()     {}
func (*Lambda) expr()    {}
func (*If) expr()        {}
func (*When) expr()      {}
func (*FuncRef) expr()   {}
func (*ClassRef) expr()  {}
func (*Jump) expr()      {}
