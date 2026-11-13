package main

import (
	"fmt"
	"math/rand"
	"reflect"
	"slices"
	"sort"
	"strings"
)

var out strings.Builder

type pair struct {
	f1 int
	f2 float64
}

type metric struct{ m map[pair]int }

func (m *metric) metrics() (r1 []int, r2 []float64) {
	for k := range m.m {
		r1 = append(r1, k.f1)
		r2 = append(r2, k.f2)
	}
	return r1, r2
}

type T struct{ base []int }

func (T) two() (r1 []int, r2 int)           { return []int{3, 1, 2}, 0 }
func (T) one() []int                        { return []int{3, 1, 2} }
func (*T) withErr() ([]int, error)          { return []int{3, 1, 2}, nil }
func (T) uints() ([]uint, int)              { return []uint{3, 1, 2}, 0 }
func (T) int64s() ([]int64, int)            { return []int64{3, 1, 2}, 0 }
func (T) floats() ([]float64, int)          { return []float64{3, 1, 2}, 0 }
func (T) strs() ([]string, int)             { return []string{"c", "a", "b"}, 0 }
func (t T) field() ([]int, int)             { return t.base, 0 }
func (T) three() (a []int, b []int, c bool) { return []int{3, 1, 2}, []int{6, 5, 4}, true }

func two() ([]int, int)      { return []int{3, 1, 2}, 0 }
func uints() ([]uint, int)   { return []uint{3, 1, 2}, 0 }
func int64s() ([]int64, int) { return []int64{3, 1, 2}, 0 }
func narrow() (int32, uint8) { return 7, 8 }

type Getter interface{ two() ([]int, int) }
type One interface{ one() []int }

type byValue []string

func (b byValue) Len() int           { return len(b) }
func (b byValue) Less(i, j int) bool { return b[i] < b[j] }
func (b byValue) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }

func check(label string, value any) {
	fmt.Fprintf(&out, "%s: %T %v\n", label, value, value)
}

func run() string {
	m := &metric{m: map[pair]int{}}
	for i := 9; i > 0; i-- {
		m.m[pair{i, float64(i)}]++
	}
	k1, k2 := m.metrics()
	sort.Ints(k1)
	sort.Float64s(k2)
	check("map range method", k1)
	check("map range method floats", k2)

	var t T
	a, _ := t.two()
	sort.Ints(a)
	check("sort.Ints", a)

	b, _ := (&T{}).withErr()
	slices.Sort(b)
	check("slices.Sort", b)

	c, _ := t.uints()
	slices.Sort(c)
	check("uints", c)

	d, _ := t.int64s()
	slices.Sort(d)
	check("int64s", d)

	f, _ := t.floats()
	sort.Float64s(f)
	check("floats", f)

	s, _ := t.strs()
	sort.Sort(byValue(s))
	check("sort.Sort", s)

	owner := T{base: []int{9, 8, 7}}
	alias, _ := owner.field()
	sort.Ints(alias)
	check("field alias", owner.base)

	var g Getter = t
	gi, _ := g.two()
	sort.Ints(gi)
	check("interface, two results", gi)

	var o One = t
	oi := o.one()
	sort.Ints(oi)
	check("interface, one result", oi)

	value := t.two
	mv, _ := value()
	sort.Ints(mv)
	check("method value", mv)

	expression := T.two
	me, _ := expression(t)
	sort.Ints(me)
	check("method expression", me)

	first, second, _ := t.three()
	sort.Ints(first)
	sort.Ints(second)
	check("three results", fmt.Sprint(first, second))

	var x, y []int
	x, _ = t.two()
	y = x
	sort.Ints(x)
	check("assigned alias", y)

	sh, _ := t.two()
	rand.New(rand.NewSource(1)).Shuffle(len(sh), func(i, j int) { sh[i], sh[j] = sh[j], sh[i] })
	sort.Ints(sh)
	check("shuffle then sort", sh)

	sl, _ := t.two()
	sort.Slice(sl, func(i, j int) bool { return sl[i] < sl[j] })
	check("sort.Slice", sl)

	rv, _ := t.two()
	reflect.Copy(reflect.ValueOf(rv), reflect.ValueOf([]int{7}))
	check("reflect.Copy", rv)

	captured, _ := two()
	func() { sort.Ints(captured) }()
	check("captured function result", captured)

	capturedMethod, _ := t.two()
	func() { sort.Ints(capturedMethod) }()
	check("captured method result", capturedMethod)

	sent, _ := t.two()
	ch := make(chan []int, 1)
	ch <- sent
	sort.Ints(<-ch)
	check("sent on a channel", sent)

	done := make(chan bool)
	inGoroutine, _ := t.two()
	go func() { sort.Ints(inGoroutine); done <- true }()
	<-done
	check("goroutine", inGoroutine)

	var boxed any
	boxed, _ = t.two()
	sort.Ints(boxed.([]int))
	check("any", boxed)

	fu, _ := uints()
	check("function uints", fu)
	var assigned []uint
	assigned, _ = uints()
	check("assigned uints", assigned)
	fi, _ := int64s()
	check("function int64s", fi)
	n1, n2 := narrow()
	check("narrow", fmt.Sprintf("%T %T", n1, n2))
	return out.String()
}
