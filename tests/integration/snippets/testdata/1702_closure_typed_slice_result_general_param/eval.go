package main

import "fmt"

func run() string {
	fs := []func() int{func() int { return 1 }, func() int { return 2 }}
	collect := func(list []func() int) []int {
		out := []int{}
		for _, f := range list {
			out = append(out, f())
		}
		return out
	}
	names := func(list []func() int) []string {
		out := []string{}
		for range list {
			out = append(out, "x")
		}
		return out
	}
	single := func(list []func() int) []int {
		out := []int{7}
		return out
	}
	return fmt.Sprint(collect(fs), names(fs), single(fs))
}
