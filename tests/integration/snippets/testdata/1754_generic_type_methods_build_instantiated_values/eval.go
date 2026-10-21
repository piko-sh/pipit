package main

import (
	"bytes"
	"fmt"
	"strings"
)

type node[K, V any] struct {
	key         K
	val         V
	left, right *node[K, V]
}

type orderedMap[K, V any] struct {
	root    *node[K, V]
	compare func(K, K) int
}

func (m *orderedMap[K, V]) find(key K) **node[K, V] {
	pn := &m.root
	for *pn != nil {
		switch c := m.compare(key, (*pn).key); {
		case c < 0:
			pn = &(*pn).left
		case c > 0:
			pn = &(*pn).right
		default:
			return pn
		}
	}
	return pn
}

func (m *orderedMap[K, V]) insert(key K, val V) bool {
	pn := m.find(key)
	if *pn != nil {
		(*pn).val = val
		return false
	}
	*pn = &node[K, V]{key: key, val: val}
	return true
}

func (m *orderedMap[K, V]) lookup(key K) (V, bool) {
	pn := m.find(key)
	if *pn == nil {
		var zero V
		return zero, false
	}
	return (*pn).val, true
}

func (m *orderedMap[K, V]) walk(visit func(K, V)) {
	var step func(*node[K, V])
	step = func(n *node[K, V]) {
		if n == nil {
			return
		}
		step(n.left)
		visit(n.key, n.val)
		step(n.right)
	}
	step(m.root)
}

type item[T any] struct {
	value T
	next  *item[T]
}

type stack[T any] struct {
	top  *item[T]
	size int
}

func (s *stack[T]) push(value T) {
	s.top = &item[T]{value: value, next: s.top}
	s.size++
}

func (s *stack[T]) pop() (T, bool) {
	if s.top == nil {
		var zero T
		return zero, false
	}
	value := s.top.value
	s.top = s.top.next
	s.size--
	return value, true
}

type cell[K any] struct {
	key  K
	next *cell[K]
}

type rooted[K any] struct {
	root *cell[K]
	n    int
}

func (r *rooted[K]) rootAddress() **cell[K] { return &r.root }

type rootAddresser interface{ rootAddress() **cell[int] }

type namedStack struct {
	stack[int]
	name string
}

type pointerStack struct{ *stack[string] }

type iterator[T any] interface{ iterate(fn func(T) bool) }

type iteratorFunc[T any] func(fn func(T) bool)

func (f iteratorFunc[T]) iterate(fn func(T) bool) { f(fn) }

func iterateOver[T any](values []T) iterator[T] {
	return iteratorFunc[T](func(fn func(T) bool) {
		for _, value := range values {
			if !fn(value) {
				return
			}
		}
	})
}

func run() string {
	var out strings.Builder

	m := &orderedMap[[]byte, int]{compare: bytes.Compare}
	_, found := m.lookup([]byte("a"))
	fmt.Fprintln(&out, "empty", found)
	for _, key := range []string{"m", "c", "x", "a", "c"} {
		fmt.Fprintln(&out, "insert", key, m.insert([]byte(key), len(key)+int(key[0])))
	}
	value, found := m.lookup([]byte("c"))
	fmt.Fprintln(&out, "lookup", value, found)
	m.walk(func(key []byte, value int) { fmt.Fprintf(&out, "%s=%d ", key, value) })
	fmt.Fprintln(&out)

	s := &stack[string]{}
	push := s.push
	push("a")
	(*stack[string]).push(s, "b")
	s.push("c")
	for {
		value, ok := s.pop()
		if !ok {
			break
		}
		fmt.Fprint(&out, value, " ")
	}
	fmt.Fprintln(&out, s.size)

	numbers := &stack[float64]{}
	numbers.push(1.5)
	top, _ := numbers.pop()
	fmt.Fprintln(&out, "float", top)

	named := &namedStack{name: "n"}
	named.push(1)
	named.push(2)
	pushNamed := named.push
	pushNamed(3)
	pointer := pointerStack{&stack[string]{}}
	pointer.push("p")
	fmt.Fprintln(&out, "embedded", named.top.value, named.top.next.value, named.size, pointer.top.value)

	sum := 0
	iterateOver([]int{1, 2, 3}).iterate(func(value int) bool { sum += value; return true })
	fmt.Fprintln(&out, "named func", sum)

	r := &rooted[int]{n: 1}
	fmt.Fprintln(&out, "direct", *r.rootAddress() == nil)
	var addresser rootAddresser = &rooted[int]{n: 2}
	fmt.Fprintln(&out, "interface", *addresser.rootAddress() == nil)
	*addresser.rootAddress() = &cell[int]{key: 4}
	fmt.Fprintln(&out, "stored", addresser.(*rooted[int]).root.key)

	return out.String()
}
