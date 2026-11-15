package kogo

import "kogo/rt"

// Either and its two variants are check/core.kg's, written here as the
// emitter would write that sealed interface. Each writes itself as
// Arrow does, Either.Left(value).
type Either[E, A any] interface{}

type Either_Left[E any] struct {
	_     struct{} `kogo:"Left"`
	Value E        `kogo:"value"`
}

type Either_Right[A any] struct {
	_     struct{} `kogo:"Right"`
	Value A        `kogo:"value"`
}

func (l Either_Left[E]) String() string  { return "Either.Left(" + rt.ToString(l.Value) + ")" }
func (r Either_Right[A]) String() string { return "Either.Right(" + rt.ToString(r.Value) + ")" }

// A failure (spec.md section 9) is a panic that its own raise scope
// recovers (decisions.md). A Raise is a scope's handle: each block of
// either is given a new one, and every failure carries the handle it
// was raised on, so a scope recovers its own failures and lets any
// other panic pass, another scope's failure or a bug (section 8).
type Raise[E any] struct{ scope *scope }

type scope struct{ _ byte } // not empty: Go may give every new empty value one address

type failure struct {
	scope *scope
	value any
}

// Error is seen only when a failure outlives its scope, which the
// checker's rules for lambdas prevent.
func (f failure) Error() string {
	return "kogo: a failure outside its raise scope: " + rt.ToString(f.value)
}

// Either_RaiseEToA is the core's either: it runs block in a new raise
// scope, and gives its value, or the failure raised on its handle.
func Either_RaiseEToA[E, A any](block func(Raise[E]) A) (result Either[E, A]) {
	r := Raise[E]{&scope{}}
	defer func() {
		p := recover()
		if f, ok := p.(failure); ok && f.scope == r.scope {
			result = Either_Left[E]{Value: f.value.(E)}
		} else if p != nil {
			panic(p)
		}
	}()
	return Either_Right[A]{Value: block(r)}
}

// The forms of a raise scope are given the handle of the scope they
// are called in, and its E, to which the emitter converts the value.
func Raise_Raise[E any](this Raise[E], r E) rt.Nothing { panic(failure{this.scope, r}) }

func Raise_Ensure[E any](this Raise[E], condition bool, raise func() E) rt.Unit {
	if !condition {
		Raise_Raise(this, raise())
	}
	return rt.Unit{}
}

func Raise_EnsureNotNull[E, A any](this Raise[E], value *A, raise func() E) A {
	if value == nil {
		Raise_Raise(this, raise())
	}
	return *value
}

// Either_Bind is x.bind(): the scope's E may be wider than x's, and
// widen gives a failure of x's as one of the scope's.
func Either_Bind[E, A, F any](this Either[E, A], scope Raise[F], widen func(E) F) A {
	if l, ok := this.(Either_Left[E]); ok {
		Raise_Raise(scope, widen(l.Value))
	}
	return this.(Either_Right[A]).Value
}

// Either_widen gives an Either<E, A> as an Either<F, B>, when E and A
// are used as wider types: it is rebuilt, so that an is test finds it.
func Either_widen[E, A, F, B any](this Either[E, A], f func(E) F, g func(A) B) Either[F, B] {
	if l, ok := this.(Either_Left[E]); ok {
		return Either_Left[F]{Value: f(l.Value)}
	}
	return Either_Right[B]{Value: g(this.(Either_Right[A]).Value)}
}

func A_Right[A any](this A) Either[rt.Nothing, A] { return Either_Right[A]{Value: this} }
func E_Left[E any](this E) Either[E, rt.Nothing]  { return Either_Left[E]{Value: this} }

func Either_Fold[E, A, R any](this Either[E, A], ifLeft func(E) R, ifRight func(A) R) R {
	if l, ok := this.(Either_Left[E]); ok {
		return ifLeft(l.Value)
	}
	return ifRight(this.(Either_Right[A]).Value)
}

func Either_MapLeft[E, A, F any](this Either[E, A], f func(E) F) Either[F, A] {
	return Either_Fold(this, func(e E) Either[F, A] { return Either_Left[F]{Value: f(e)} }, func(A) Either[F, A] { return this })
}

func Either_Map[E, A, B any](this Either[E, A], f func(A) B) Either[E, B] {
	return Either_FlatMap(this, func(a A) Either[E, B] { return Either_Right[B]{Value: f(a)} })
}

func Either_FlatMap[E, A, B any](this Either[E, A], f func(A) Either[E, B]) Either[E, B] {
	return Either_Fold(this, func(E) Either[E, B] { return this }, f)
}

func Either_GetOrElse[E, A any](this Either[E, A], default_ func(E) A) A {
	return Either_Fold(this, default_, func(a A) A { return a })
}

func Either_GetOrNull[E, A any](this Either[E, A]) *A {
	return Either_Fold(this, func(E) *A { return nil }, func(a A) *A { return &a })
}
