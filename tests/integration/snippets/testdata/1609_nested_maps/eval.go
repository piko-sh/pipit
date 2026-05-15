package main

import (
	"fmt"
	"sort"
)

func run() string {
	m := map[string]map[string]int{}
	m["a"] = map[string]int{}
	m["a"]["b"] = 1
	m["a"]["c"] += 2
	inner := m["a"]
	keys := make([]string, 0, len(inner))
	for k := range inner {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	nested := map[int]map[int]bool{1: {2: true}}
	chans := map[string]chan int{"c": make(chan int, 1)}
	chans["c"] <- 5
	funcs := map[string]func(int) int{"double": func(x int) int { return 2 * x }}
	_, missing := m["zzz"]["q"]
	return fmt.Sprint(len(inner), " ", inner["b"], " ", m["a"]["c"], " ", keys, " ", nested[1][2], " ",
		len(nested[1]), " ", <-chans["c"], " ", funcs["double"](21), " ", missing, " ", len(m["zzz"]))
}
