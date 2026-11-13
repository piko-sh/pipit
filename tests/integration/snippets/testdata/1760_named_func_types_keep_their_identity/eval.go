package main

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

var out strings.Builder

type Namer interface{ Name() string }

type A func() int

func (a A) String() string { return fmt.Sprintf("A%d", a()) }
func (a A) Name() string   { return "A" }

type B func() int

func (b B) String() string { return fmt.Sprintf("B%d", b()) }
func (b B) Name() string   { return "B" }

type E func() int

func (e E) Error() string { return "E-error" }

type Fm func() int

func (f Fm) Format(s fmt.State, c rune) { fmt.Fprintf(s, "Fm<%c>", c) }

type A2 A

type G[T any] func() int

func (g G[T]) Name() string { return "G" }

type Caller interface{ Call() int }

type F func() int

func (f F) Call() int { return f() + 100 }

type K func() int

func (k K) Call() int { return k() + 200 }

type Chain func()

func (c Chain) Run() string {
	if c != nil {
		c()
	}
	return "ran"
}

func (c Chain) Then() {}

var global any = B(func() int { return 2 })
var globalNamer Namer = A(func() int { return 1 })

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
	case func() int:
		return "unnamed"
	default:
		return "other"
	}
}

func run() string {
	out.Reset()
	a, b := A(func() int { return 1 }), B(func() int { return 2 })
	var xa, xb, xu any = a, b, func() int { return 3 }
	check("fmt direct", func() string { return fmt.Sprint(a, " ", b) })
	check("fmt any", func() string { return fmt.Sprint(xa, " ", xb) })
	check("fmt %T", func() string { return fmt.Sprintf("%T %T %T %s %v", xa, xb, xu, xa, xb) })
	check("Error any", func() string {
		var e error = E(func() int { return 3 })
		var x any = e
		return fmt.Sprint(x) + " " + e.Error()
	})
	check("Format", func() string {
		var x any = Fm(func() int { return 3 })
		return fmt.Sprintf("%v %d %s", Fm(func() int { return 3 }), x, x)
	})
	check("iface stored", func() string { var n1 Namer = a; var n2 Namer = b; return n1.Name() + n2.Name() })
	check("iface slice", func() string {
		ns := []Namer{a, b, G[int](func() int { return 3 })}
		s := ""
		for _, n := range ns {
			s += n.Name()
		}
		return s
	})
	check("iface map/struct", func() string {
		m := map[string]Namer{"a": a, "b": b}
		h := holder{n: b}
		return m["a"].Name() + m["b"].Name() + h.n.Name()
	})
	check("any to iface", func() string { return xa.(Namer).Name() + xb.(Namer).Name() })
	check("stringer", func() string { return xa.(fmt.Stringer).String() + xb.(fmt.Stringer).String() })
	check("type switch", func() string {
		return kind(a) + kind(b) + kind(xu) + kind(A2(func() int { return 3 })) + kind(G[int](func() int { return 3 })) + kind(G[string](func() int { return 3 }))
	})
	check("assert", func() string {
		_, ok1 := xa.(A)
		_, ok2 := xa.(B)
		_, ok3 := xa.(func() int)
		_, ok4 := xu.(A)
		_, ok5 := xa.(fmt.Stringer)
		_, ok6 := xu.(fmt.Stringer)
		_, ok7 := any(A2(func() int { return 3 })).(Namer)
		return fmt.Sprint(ok1, ok2, ok3, ok4, ok5, ok6, ok7)
	})
	check("assert panic", func() string { _ = xa.(B); return "no panic" })
	check("errors.As", func() string {
		var err error = fmt.Errorf("wrap: %w", E(func() int { return 3 }))
		var target E
		ok := errors.As(err, &target)
		return fmt.Sprint(ok, kind(target))
	})
	check("eq any", func() string { return fmt.Sprint(xa == xu) })
	check("reflect", func() string {
		t := reflect.TypeOf(xa)
		return fmt.Sprint(t.Name(), " ", t.String(), " ", t.Kind(), " ", t.NumMethod(), " ", reflect.TypeOf(xu).Name(), reflect.TypeOf(xa) == reflect.TypeOf(xb))
	})
	check("reflect method", func() string {
		m := reflect.ValueOf(xb).MethodByName("Name")
		if !m.IsValid() {
			return "no method"
		}
		return m.Call(nil)[0].String()
	})
	check("sync.Map", func() string {
		var m sync.Map
		m.Store("a", a)
		m.Store("b", b)
		va, _ := m.Load("a")
		vb, _ := m.Load("b")
		return va.(Namer).Name() + vb.(Namer).Name() + kind(va) + kind(vb)
	})
	check("reflect roundtrip", func() string {
		y := reflect.ValueOf(xb).Interface()
		return y.(Namer).Name() + kind(y)
	})
	check("conv", func() string {
		raw := (func() int)(a)
		back := B(raw)
		var x any = back
		return kind(x) + kind(raw) + kind(A(back))
	})
	check("goroutine chan", func() string {
		ch := make(chan any, 2)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() { defer wg.Done(); ch <- a; ch <- b }()
		wg.Wait()
		v1, v2 := <-ch, <-ch
		return kind(v1) + kind(v2) + v1.(Namer).Name() + v2.(Namer).Name()
	})
	check("closure global", func() string {
		f := func() any { return b }
		return kind(f()) + kind(global) + globalNamer.Name()
	})
	check("named of named", func() string {
		var x any = A2(func() int { return 3 })
		_, isStringer := x.(fmt.Stringer)
		return fmt.Sprintf("%T %v %s", x, isStringer, kind(x))
	})
	check("generic", func() string {
		var x, y any = G[int](func() int { return 3 }), G[string](func() int { return 3 })
		return fmt.Sprintf("%T %T %s %s %s", x, y, x.(Namer).Name(), y.(Namer).Name(), reflect.TypeOf(x).Name())
	})
	check("method dispatch by named func type", func() string {
		f := F(func() int { return 1 })
		var c Caller = f
		var d Caller = K(func() int { return 2 })
		return fmt.Sprint(f.Call(), c.Call(), d.Call())
	})
	check("method value of named func type", func() string {
		f := F(func() int { return 3 })
		mv := f.Call
		var c Chain = func() {}
		c = c.Then
		return fmt.Sprint(mv(), c.Run())
	})
	check("assert named func types", func() string {
		var x any = F(func() int { return 1 })
		_, isF := x.(F)
		_, isK := x.(K)
		_, isPlain := x.(func() int)
		return fmt.Sprintf("%T %v %v %v", x, isF, isK, isPlain)
	})
	return out.String()
}
