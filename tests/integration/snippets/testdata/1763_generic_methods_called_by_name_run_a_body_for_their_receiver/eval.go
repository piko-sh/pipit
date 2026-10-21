package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

var out strings.Builder

type node[K comparable, V any] struct {
	key   K
	value V
	next  *node[K, V]
}

type Map[K comparable, V any] struct {
	head *node[K, V]
	size int
}

func (m *Map[K, V]) Put(k K, v V) {
	for n := m.head; n != nil; n = n.next {
		if n.key == k {
			n.value = v
			return
		}
	}
	m.head = &node[K, V]{key: k, value: v, next: m.head}
	m.size++
}

func (m *Map[K, V]) Get(k K) (V, bool) {
	for n := m.head; n != nil; n = n.next {
		if n.key == k {
			return n.value, true
		}
	}
	var zero V
	return zero, false
}

func (m *Map[K, V]) Keys() []K {
	keys := make([]K, 0, m.size)
	for n := m.head; n != nil; n = n.next {
		keys = append(keys, n.key)
	}
	return keys
}

func (m *Map[K, V]) Fresh() *V        { return new(V) }
func (m *Map[K, V]) Describe() string { var k K; var v V; return fmt.Sprintf("%T/%T", k, v) }
func (m *Map[K, V]) Cast(x any) V     { return x.(V) }

type Store[K comparable, V any] interface {
	Put(K, V)
	Get(K) (V, bool)
	Keys() []K
	Fresh() *V
	Describe() string
}

type Opt[T any] struct {
	v  T
	ok bool
}

func (o Opt[T]) String() string {
	if !o.ok {
		return "none"
	}
	return fmt.Sprintf("some(%v:%T)", o.v, o.v)
}

func (o Opt[T]) MarshalJSON() ([]byte, error) {
	if !o.ok {
		return []byte("null"), nil
	}
	return json.Marshal([]T{o.v})
}

type Err[T any] struct{ detail T }

func (e *Err[T]) Error() string { return fmt.Sprintf("err[%T]=%v", e.detail, e.detail) }

type Sorted[T any] struct {
	items []T
	less  func(a, b T) bool
}

func (s *Sorted[T]) Len() int           { return len(s.items) }
func (s *Sorted[T]) Less(i, j int) bool { return s.less(s.items[i], s.items[j]) }
func (s *Sorted[T]) Swap(i, j int)      { s.items[i], s.items[j] = s.items[j], s.items[i] }

type Named[T any] struct {
	Map[string, T]
	label string
}

type Pair[A, B any] struct {
	a A
	b B
}

func (p Pair[A, B]) Swap() Pair[B, A] { return Pair[B, A]{a: p.b, b: p.a} }
func (p Pair[A, B]) Kinds() string    { return fmt.Sprintf("%T,%T", p.a, p.b) }

type Swapper[A, B any] interface{ Swap() Pair[B, A] }
type Kinder interface{ Kinds() string }

type Fn[T any] func(T) T

func (f Fn[T]) Twice(x T) T { return f(f(x)) }

type Twicer[T any] interface{ Twice(T) T }

func newStore[V any]() Store[string, V] { return &Map[string, V]{} }

func check(label string, value any) { fmt.Fprintf(&out, "%s: %v\n", label, value) }

func run() string {
	var s Store[string, int] = &Map[string, int]{}
	s.Put("a", 1)
	s.Put("b", 2)
	s.Put("a", 3)
	v, ok := s.Get("a")
	check("get", fmt.Sprint(v, ok))
	keys := s.Keys()
	sort.Strings(keys)
	check("keys", keys)
	check("fresh", fmt.Sprintf("%T %v", s.Fresh(), *s.Fresh()))
	check("describe", s.Describe())

	put := s.Put
	put("c", 4)
	check("method value", len(s.Keys()))
	get := Store[string, int].Get
	gv, gok := get(s, "c")
	check("method expression", fmt.Sprint(gv, gok))
	pointerExpr := (*Map[string, int]).Get
	pv, _ := pointerExpr(s.(*Map[string, int]), "b")
	check("pointer method expression", pv)

	var structs Store[int, Opt[string]] = &Map[int, Opt[string]]{}
	structs.Put(1, Opt[string]{v: "x", ok: true})
	ov, _ := structs.Get(1)
	check("nested generic", ov)
	check("nested zero", fmt.Sprintf("%v %T", *structs.Fresh(), *structs.Fresh()))
	check("nested describe", structs.Describe())

	var stringer fmt.Stringer = Opt[[]int]{v: []int{1, 2}, ok: true}
	check("stringer", stringer)
	encoded, err := json.Marshal(map[string]any{"o": Opt[int]{v: 7, ok: true}, "n": Opt[bool]{}})
	check("json", string(encoded)+fmt.Sprint(err))

	var e error = &Err[int]{detail: 42}
	check("error", e)
	wrapped := fmt.Errorf("wrap: %w", e)
	var target *Err[int]
	check("errors.As", fmt.Sprint(errors.As(wrapped, &target), target.detail))

	sorted := &Sorted[string]{items: []string{"pear", "fig", "apple"}, less: func(a, b string) bool { return len(a) < len(b) }}
	sort.Sort(sorted)
	check("sort.Sort", sorted.items)

	var embedded Store[string, float64] = &Named[float64]{label: "n"}
	embedded.Put("z", 1.5)
	ev, _ := embedded.Get("z")
	check("embedded", fmt.Sprint(embedded.Keys(), ev, embedded.Describe()))

	var swapper Swapper[int, string] = Pair[int, string]{a: 1, b: "one"}
	swapped := swapper.Swap()
	check("swap", fmt.Sprintf("%v %T", swapped, swapped))
	var kinder Kinder = swapped
	check("kinds", kinder.Kinds())

	var twicer Twicer[int] = Fn[int](func(x int) int { return x * 3 })
	check("func type", twicer.Twice(2))

	fromGeneric := newStore[[]byte]()
	fromGeneric.Put("k", []byte("v"))
	fb, _ := fromGeneric.Get("k")
	check("from generic function", fmt.Sprintf("%s %s", fb, fromGeneric.Describe()))

	var wg sync.WaitGroup
	shared := Store[int, string](&Map[int, string]{})
	var mu sync.Mutex
	for i := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.Lock()
			shared.Put(i, strings.Repeat("x", i))
			mu.Unlock()
		}()
	}
	wg.Wait()
	sk := shared.Keys()
	sort.Ints(sk)
	check("goroutines", sk)

	rv := reflect.ValueOf(s).MethodByName("Describe").Call(nil)
	check("reflect call", rv[0])

	var anyStore any = s
	switch typed := anyStore.(type) {
	case Store[string, int]:
		check("type switch", typed.Describe())
	default:
		check("type switch", "miss")
	}
	return out.String()
}
