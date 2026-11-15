package kogo

import "kogo/rt"

// Either and its two variants are check/core.kg's, written here as the
// emitter would write that sealed interface.
type Either[E, A any] interface{}

type Either_Left[E any] struct {
	_     struct{} `kogo:"Left"`
	Value E        `kogo:"value"`
}

type Either_Right[A any] struct {
	_     struct{} `kogo:"Right"`
	Value A        `kogo:"value"`
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
