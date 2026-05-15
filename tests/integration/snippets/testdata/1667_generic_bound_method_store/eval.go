package main

import "fmt"

type box[T any] struct{ a T }

func (x *box[T]) set(v T) { x.a = v }

func (x *box[T]) get() T { return x.a }

type pair[K comparable, V any] struct {
	key   K
	value V
}

func (p *pair[K, V]) put(k K, v V) {
	p.key = k
	p.value = v
}

func run() string {
	ints := &box[int]{}
	set := ints.set
	set(7)

	strings := &box[string]{}
	strings.set("x")

	two := &pair[string, float64]{}
	put := two.put
	put("k", 1.5)

	return fmt.Sprintf("%d/%T %s/%T %s=%v/%T", ints.get(), ints.a, strings.get(), strings.a, two.key, two.value, two.value)
}
