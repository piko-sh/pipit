package main

import (
	"fmt"
	"runtime"
	"strings"
)

var out strings.Builder

func three(yield func(int) bool) {
	yield(1)
	yield(2)
	yield(3)
}

func pairs(yield func(string, int) bool) {
	for i, s := range []string{"a", "b", "c"} {
		if !yield(s, i) {
			yield("again", -1)
			return
		}
	}
}

func polite(yield func(int) bool) {
	for i := range 4 {
		if !yield(i) {
			return
		}
	}
}

var saved func(int) bool

func keeper(yield func(int) bool) {
	saved = yield
	yield(10)
}

func catch(name string, run func()) {
	defer func() {
		r := recover()
		_, isRuntime := r.(runtime.Error)
		fmt.Fprintln(&out, name, r, isRuntime)
	}()
	run()
}

func firstOf() (result int) {
	for v := range three {
		return v * 100
	}
	return -1
}

func run() string {
	catch("break", func() {
		for v := range three {
			fmt.Fprintln(&out, "body", v)
			if v == 1 {
				break
			}
		}
	})
	catch("continue", func() {
		for v := range three {
			if v == 2 {
				continue
			}
			fmt.Fprintln(&out, "body", v)
		}
	})
	catch("return", func() { fmt.Fprintln(&out, firstOf()) })
	catch("pairs", func() {
		for s, i := range pairs {
			if i == 1 {
				break
			}
			fmt.Fprintln(&out, "pair", s, i)
		}
	})
	catch("polite", func() {
		for v := range polite {
			if v == 2 {
				break
			}
			fmt.Fprintln(&out, "polite", v)
		}
	})
	catch("labelled", func() {
	outer:
		for range polite {
			for v := range three {
				if v == 1 {
					continue outer
				}
			}
		}
	})
	catch("labelled polite", func() {
	outer:
		for a := range polite {
			for b := range polite {
				if b == 1 {
					continue outer
				}
				if a == 2 {
					break outer
				}
				fmt.Fprint(&out, a, b, " ")
			}
			fmt.Fprint(&out, "unreached ")
		}
		fmt.Fprintln(&out)
	})
	catch("labelled deep", func() {
	top:
		for a := range polite {
			for b := range polite {
				for c := range polite {
					if c == 1 {
						continue top
					}
					if a == 1 && b == 0 {
						break top
					}
					fmt.Fprint(&out, a, b, c, " ")
				}
			}
		}
		fmt.Fprintln(&out)
	})
	catch("after exit", func() {
		for v := range keeper {
			fmt.Fprintln(&out, "kept", v)
		}
		saved(11)
	})
	catch("earlier execution", func() {
		for round := range 2 {
			for v := range keeper {
				fmt.Fprintln(&out, "round", round, v)
				if round == 1 {
					break
				}
			}
		}
		saved(12)
	})
	catch("nested", func() {
		for a := range polite {
			for b := range polite {
				if b > a {
					break
				}
				fmt.Fprint(&out, a, b, " ")
			}
		}
		fmt.Fprintln(&out)
	})
	return out.String()
}
