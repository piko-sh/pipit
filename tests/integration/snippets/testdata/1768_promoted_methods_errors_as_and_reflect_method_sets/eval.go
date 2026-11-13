package main

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

var out strings.Builder

type Namer interface{ Name() string }

type Base struct{ n string }

func (b Base) Name() string { return "base:" + b.n }

type Wrap struct {
	Namer
	extra int
}

type PtrBase struct{ n string }

func (b *PtrBase) Name() string { return "ptr:" + b.n }

type Outer struct {
	*PtrBase
}

type Deep struct {
	Wrap
}

type MyErr struct{ Code int }

func (e *MyErr) Error() string { return fmt.Sprintf("myerr %d", e.Code) }

type ValErr struct{ Code int }

func (e ValErr) Error() string { return "valerr" }

type CodeErr int

func (c CodeErr) Error() string { return fmt.Sprintf("code %d", int(c)) }

type ListErr []string

func (l ListErr) Error() string { return strings.Join(l, ";") }

type G[T any] struct{ v T }

func (g G[T]) Get() T              { return g.v }
func (g G[T]) Put(v T, n int) G[T] { return G[T]{v} }

type Getter[T any] interface{ Get() T }

type P struct{ Name string }

func (p P) String() string  { return "P" }
func (p P) Hello(n int) int { return n + 1 }
func (p *P) Set(s string)   { p.Name = s }

type L []int

func (l L) String() string { return "L" }

type N int

func (n N) String() string { return "N" }

type Plain struct{ x int }

func check(label string, f func() string) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(&out, label, "PANIC", r)
		}
	}()
	fmt.Fprintln(&out, label, f())
}

func run() string {
	check("promoted-interface", func() string {
		var n Namer = Wrap{Namer: Base{"x"}}
		var n2 Namer = Outer{&PtrBase{"y"}}
		var n3 Namer = Deep{Wrap{Namer: &PtrBase{"z"}}}
		w := Wrap{Namer: Base{"w"}}
		return n.Name() + " " + n2.Name() + " " + n3.Name() + " " + w.Name()
	})
	check("errors-as", func() string {
		err := fmt.Errorf("wrapped: %w", &MyErr{Code: 7})
		var target *MyErr
		ok := errors.As(err, &target)
		var ve ValErr
		ok2 := errors.As(fmt.Errorf("w: %w", ValErr{1}), &ve)
		var ce CodeErr
		ok3 := errors.As(fmt.Errorf("c: %w", CodeErr(4)), &ce)
		var le ListErr
		ok4 := errors.As(fmt.Errorf("l: %w", ListErr{"a", "b"}), &le)
		var e2 error = &MyErr{3}
		return fmt.Sprint(ok, target != nil && target.Code == 7, ok2, ve.Code, ok3, int(ce), ok4, len(le), errors.Is(fmt.Errorf("x %w", e2), e2))
	})
	check("generic-interface", func() string {
		var g Getter[int] = G[int]{5}
		return fmt.Sprint(g.Get())
	})
	check("generic-reflect-signatures", func() string {
		m, ok := reflect.TypeOf(G[int]{}).MethodByName("Get")
		put, _ := reflect.TypeOf(G[string]{}).MethodByName("Put")
		bound := reflect.ValueOf(G[float64]{2.5}).MethodByName("Get")
		return fmt.Sprint(ok, " ", m.Type, " ", reflect.TypeOf(G[string]{}).Method(0).Type, " ", put.Type, " ", bound.Type(), " ", bound.Call(nil)[0])
	})
	check("nummethod", func() string {
		return fmt.Sprint(reflect.TypeOf(P{}).NumMethod(), reflect.TypeOf(&P{}).NumMethod(), reflect.TypeOf(L{}).NumMethod(), reflect.TypeOf(N(0)).NumMethod())
	})
	check("methodbyname-call", func() string {
		r := reflect.ValueOf(P{}).MethodByName("Hello").Call([]reflect.Value{reflect.ValueOf(4)})
		return fmt.Sprint(r[0].Int())
	})
	check("implements", func() string {
		st := reflect.TypeOf((*fmt.Stringer)(nil)).Elem()
		et := reflect.TypeOf((*error)(nil)).Elem()
		nt := reflect.TypeOf((*Namer)(nil)).Elem()
		return fmt.Sprint(reflect.TypeOf(P{}).Implements(st), reflect.TypeOf(L{}).Implements(st), reflect.TypeOf(N(1)).Implements(st),
			reflect.TypeOf(P{}).Implements(et), reflect.TypeOf(Plain{}).Implements(st), reflect.TypeOf(CodeErr(0)).Implements(et),
			reflect.TypeOf(&MyErr{}).Implements(et), reflect.TypeOf(Base{}).Implements(nt), reflect.TypeOf(P{}).Implements(nt))
	})
	check("value-interface-assert", func() string {
		var x any = reflect.ValueOf(P{}).Interface()
		s, ok := x.(fmt.Stringer)
		if !ok {
			return "no"
		}
		return s.String()
	})
	return out.String()
}
