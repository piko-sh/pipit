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

type Namer interface{ Name() string }

type A []int

func (a A) String() string { return fmt.Sprintf("A%d", len(a)) }
func (a A) Name() string   { return "A" }

type B []int

func (b B) String() string { return fmt.Sprintf("B%d", len(b)) }
func (b B) Name() string   { return "B" }

type E []string

func (e E) Error() string { return "E:" + strings.Join(e, ",") }

type Fm []int

func (f Fm) Format(s fmt.State, c rune) { fmt.Fprintf(s, "Fm<%c,%d>", c, len(f)) }

type A2 A

type G[T any] []T

func (g G[T]) Name() string { return fmt.Sprintf("G%d", len(g)) }

type J []int

func (j J) MarshalJSON() ([]byte, error) { return []byte(fmt.Sprintf(`"J%d"`, len(j))), nil }

type Person struct {
	Name string
	Age  int
}
type ByAge []Person

func (a ByAge) Len() int           { return len(a) }
func (a ByAge) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByAge) Less(i, j int) bool { return a[i].Age < a[j].Age }

var global any = B{1, 2}
var globalNamer Namer = A{1}

type holder struct{ n Namer }

func check(label string, f func() string) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(&out, "%s: panic: %v\n", label, r)
		}
	}()
	fmt.Fprintf(&out, "%s: %s\n", label, f())
}

func kind(x any) string {
	switch x.(type) {
	case A:
		return "A"
	case B:
		return "B"
	case A2:
		return "A2"
	case G[int]:
		return "G[int]"
	case []int:
		return "[]int"
	default:
		return "other"
	}
}

func run() string {
	out.Reset()
	a, b := A{1}, B{1, 2}
	var xa, xb, xi any = a, b, []int{1, 2, 3}
	check("fmt direct", func() string { return fmt.Sprint(a, " ", b) })
	check("fmt any", func() string { return fmt.Sprint(xa, " ", xb, " ", xi) })
	check("fmt %v %s %T", func() string { return fmt.Sprintf("%v %s %T %T %T", xa, xb, xa, xb, xi) })
	check("Error any", func() string { var e error = E{"x"}; var x any = e; return fmt.Sprint(x) + " " + e.Error() })
	check("Error fmt", func() string { return fmt.Sprintf("%v", E{"y", "z"}) })
	check("Format", func() string { var x any = Fm{1}; return fmt.Sprintf("%v %d %v", Fm{1, 2}, x, x) })
	check("iface stored", func() string {
		var n1 Namer = A{}
		var n2 Namer = B{}
		return n1.Name() + n2.Name()
	})
	check("iface slice", func() string {
		ns := []Namer{A{}, B{}, A{}, G[int]{1}}
		s := ""
		for _, n := range ns {
			s += n.Name()
		}
		return s
	})
	check("iface map/struct", func() string {
		m := map[string]Namer{"a": A{}, "b": B{}}
		h := holder{n: A{}}
		return m["a"].Name() + m["b"].Name() + h.n.Name()
	})
	check("any to iface", func() string {
		return xa.(Namer).Name() + xb.(Namer).Name()
	})
	check("type switch", func() string {
		return kind(a) + kind(b) + kind([]int{}) + kind(A2{}) + kind(G[int]{}) + kind(G[string]{})
	})
	check("assert", func() string {
		_, ok1 := xa.(A)
		_, ok2 := xa.(B)
		_, ok3 := xa.([]int)
		_, ok4 := xi.(A)
		_, ok5 := xa.(fmt.Stringer)
		_, ok6 := xi.(fmt.Stringer)
		_, ok7 := any(A2{}).(Namer)
		return fmt.Sprint(ok1, ok2, ok3, ok4, ok5, ok6, ok7)
	})
	check("assert panic", func() string { return fmt.Sprint(len(xa.(B))) })
	check("errors.As", func() string {
		var err error = fmt.Errorf("wrap: %w", E{"q"})
		var target E
		ok := errors.As(err, &target)
		return fmt.Sprint(ok, target)
	})
	check("eq any", func() string { return fmt.Sprint(xa == xb) })
	check("eq nil-ish", func() string { var p, q any = A(nil), []int(nil); return fmt.Sprint(p == q) })
	check("reflect", func() string {
		t := reflect.TypeOf(xa)
		return fmt.Sprint(t.Name(), " ", t.String(), " ", t.Kind(), " ", t.NumMethod(), " ", reflect.TypeOf(xi).Name())
	})
	check("reflect method", func() string {
		v := reflect.ValueOf(xb)
		m := v.MethodByName("Name")
		if !m.IsValid() {
			return "no method"
		}
		return m.Call(nil)[0].String()
	})
	check("reflect equal types", func() string {
		return fmt.Sprint(reflect.TypeOf(xa) == reflect.TypeOf(xb), reflect.TypeOf(xa) == reflect.TypeOf(xi))
	})
	check("sort.Sort", func() string {
		ps := []Person{{"a", 3}, {"b", 1}, {"c", 2}}
		sort.Sort(ByAge(ps))
		return fmt.Sprint(ps)
	})
	check("sort via any", func() string {
		ps := []Person{{"a", 3}, {"b", 1}, {"c", 2}}
		var x any = ByAge(ps)
		sort.Sort(x.(sort.Interface))
		return fmt.Sprint(ps)
	})
	check("json", func() string {
		d1, _ := json.Marshal(J{1, 2})
		var x any = J{1}
		d2, _ := json.Marshal(x)
		d3, _ := json.Marshal(map[string]any{"j": J{1, 2, 3}, "a": a})
		return string(d1) + string(d2) + string(d3)
	})
	check("native []T", func() string {
		e := E{"p", "q"}
		return strings.Join(e, "+") + fmt.Sprint(sort.IntsAreSorted(a))
	})
	check("sync.Map", func() string {
		var m sync.Map
		m.Store("a", A{1})
		m.Store("b", B{1})
		va, _ := m.Load("a")
		vb, _ := m.Load("b")
		return va.(Namer).Name() + vb.(Namer).Name() + fmt.Sprint(va, vb)
	})
	check("reflect roundtrip", func() string {
		y := reflect.ValueOf(xb).Interface()
		return y.(Namer).Name() + fmt.Sprint(y) + kind(y)
	})
	check("conv", func() string {
		raw := []int(a)
		back := B(raw)
		var x any = back
		return kind(x) + fmt.Sprint(x) + kind(A(back))
	})
	check("map key any", func() string {
		m := map[any]int{}
		m[fmt.Sprint(1)] = 1
		return fmt.Sprint(len(m))
	})
	check("goroutine chan", func() string {
		ch := make(chan any, 2)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() { defer wg.Done(); ch <- A{1}; ch <- B{2} }()
		wg.Wait()
		v1, v2 := <-ch, <-ch
		return kind(v1) + kind(v2) + v1.(Namer).Name() + v2.(Namer).Name() + fmt.Sprint(v1, v2)
	})
	check("closure global", func() string {
		f := func() any { return b }
		return kind(f()) + kind(global) + globalNamer.Name() + fmt.Sprint(global)
	})
	check("named of named", func() string {
		var x any = A2{1}
		_, isStringer := x.(fmt.Stringer)
		return fmt.Sprintf("%v %T %v %s", x, x, isStringer, kind(x))
	})
	check("generic", func() string {
		var x, y any = G[int]{1}, G[string]{"a", "b"}
		return fmt.Sprintf("%T %T %s %s %s", x, y, x.(Namer).Name(), y.(Namer).Name(), reflect.TypeOf(x).Name())
	})
	return out.String()
}
