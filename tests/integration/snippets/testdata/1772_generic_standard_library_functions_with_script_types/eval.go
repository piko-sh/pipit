package main

import (
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"reflect"
	"runtime"
	"strings"
	"time"
)

type point struct{ X, Y int }

type celsius float64

func (c celsius) String() string { return fmt.Sprintf("%.1fC", float64(c)) }

type notFound struct{ name string }

func (e *notFound) Error() string { return e.name + " not found" }

type tag int32

type resource struct {
	name string
	data [64]byte
}

func typeFor() {
	fmt.Fprintln(&out, reflect.TypeFor[int](), reflect.TypeFor[[]string](), reflect.TypeFor[map[string]int]())
	fmt.Fprintln(&out, reflect.TypeFor[point]().Name(), reflect.TypeFor[point]().NumField(), reflect.TypeFor[*point]().Elem().Name())
	fmt.Fprintln(&out, reflect.TypeFor[celsius]().Kind(), reflect.TypeFor[error]().Kind(), reflect.TypeFor[fmt.Stringer]().NumMethod())
	fmt.Fprintln(&out, reflect.TypeFor[point]() == reflect.TypeOf(point{}), reflect.TypeFor[celsius]() == reflect.TypeOf(celsius(1)))
}

func typeAssert() {
	n, ok := reflect.TypeAssert[int](reflect.ValueOf(7))
	fmt.Fprintln(&out, n, ok)
	s, ok := reflect.TypeAssert[string](reflect.ValueOf(7))
	fmt.Fprintf(&out, "%q %v\n", s, ok)
	p, ok := reflect.TypeAssert[point](reflect.ValueOf(point{1, 2}))
	fmt.Fprintln(&out, p, ok)
	st, ok := reflect.TypeAssert[fmt.Stringer](reflect.ValueOf(celsius(21.5)))
	fmt.Fprintln(&out, st, ok)
	_, ok = reflect.TypeAssert[fmt.Stringer](reflect.ValueOf(point{}))
	fmt.Fprintln(&out, ok)
	c, ok := reflect.TypeAssert[celsius](reflect.ValueOf(celsius(3)))
	fmt.Fprintln(&out, c, ok)
	_, ok = reflect.TypeAssert[float64](reflect.ValueOf(celsius(3)))
	fmt.Fprintln(&out, ok)
}

func asType() {
	err := fmt.Errorf("open: %w", &notFound{"config"})
	nf, ok := errors.AsType[*notFound](err)
	fmt.Fprintln(&out, nf.name, ok)
	pe, ok := errors.AsType[*fs.PathError](err)
	fmt.Fprintln(&out, pe == nil, ok)
	wrapped := fmt.Errorf("x: %w", &fs.PathError{Op: "read", Path: "/a", Err: fs.ErrNotExist})
	pe, ok = errors.AsType[*fs.PathError](wrapped)
	fmt.Fprintln(&out, pe.Path, ok)
	_, ok = errors.AsType[*notFound](nil)
	fmt.Fprintln(&out, ok)
}

func addCleanup() {
	done := make(chan string, 1)
	func() {
		r := &resource{name: "file"}
		runtime.AddCleanup(r, func(name string) { done <- name + " closed" }, r.name)
	}()
	deadline := time.After(5 * time.Second)
	for {
		runtime.GC()
		select {
		case message := <-done:
			fmt.Fprintln(&out, message)
			return
		case <-deadline:
			fmt.Fprintln(&out, "cleanup did not run")
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func cleanupStopped() {
	ran := make(chan struct{}, 1)
	r := &resource{name: "kept"}
	handle := runtime.AddCleanup(r, func(int) { ran <- struct{}{} }, 0)
	handle.Stop()
	r = nil
	for range 5 {
		runtime.GC()
	}
	select {
	case <-ran:
		fmt.Fprintln(&out, "stopped cleanup ran")
	case <-time.After(50 * time.Millisecond):
		fmt.Fprintln(&out, "stopped cleanup did not run")
	}
}

func cleanupArgEqualsPointer() {
	defer func() { fmt.Fprintln(&out, "recovered:", recover() != nil) }()
	r := &resource{}
	runtime.AddCleanup(r, func(*resource) {}, r)
}

func randN() {
	inRange := true
	for range 200 {
		v := rand.N(10)
		w := rand.N(tag(5))
		d := rand.N(3 * time.Second)
		u := rand.N(uint8(7))
		inRange = inRange && v >= 0 && v < 10 && w >= 0 && w < 5 && d >= 0 && d < 3*time.Second && u < 7
	}
	fmt.Fprintf(&out, "%v %T %T\n", inRange, rand.N(tag(5)), rand.N(time.Second))
	defer func() { fmt.Fprintln(&out, "recovered:", recover()) }()
	rand.N(0)
}

var out strings.Builder

func run() string {
	typeFor()
	typeAssert()
	asType()
	addCleanup()
	cleanupStopped()
	cleanupArgEqualsPointer()
	randN()
	return out.String()
}
