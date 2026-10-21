package main

import (
	"fmt"
	"sort"
	"strings"
)

var lines []string

func emit(values ...any) {
	lines = append(lines, fmt.Sprint(values...))
}

type holder struct {
	names []string
	count int
}

func recovered(label string, body func()) {
	defer func() {
		if r := recover(); r != nil {
			emit(label, "panicked:", r)
			return
		}
		emit(label, "finished")
	}()
	body()
}

func fillMapOfSlices(words []string) map[string][]string {
	m := map[string][]string{}
	for i := 0; i+1 < len(words); i++ {
		k := words[i]
		m[k] = append(m[k], words[i+1])
	}
	return m
}

func printMap(m map[string][]string) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		emit(k, m[k])
	}
}

func sliceOfSlices() {
	rows := make([][]int, 3)
	for i := 0; i < 9; i++ {
		row := i % 3
		rows[row] = append(rows[row], i*i)
	}
	emit(rows, len(rows[0]), cap(rows[2]) >= 3)
}

func countsAndBounds() {
	lengths := map[string]int{}
	best := []int{4, 9, 2}
	for i, word := range []string{"ash", "birch", "ash", "cedar", "ash"} {
		lengths[word] = len(word) + lengths[word]
		slot := i % 3
		best[slot] = max(best[slot], len(word)+i)
		best[slot] = min(best[slot], 9)
	}
	emit(lengths["ash"], lengths["birch"], lengths["cedar"], best)
}

func throughPointers() {
	h := &holder{}
	for _, name := range []string{"x", "y", "z"} {
		h.names = append(h.names, name)
		h.count = len(h.names)
	}
	list := []int{1}
	p := &list
	*p = append(*p, 2, 3)
	emit(h.names, h.count, list)
}

func madeAndNew() {
	tables := map[string][]int{}
	sizes := []int{2, 0, 3}
	for i, key := range []string{"a", "b", "c"} {
		tables[key] = make([]int, sizes[i])
	}
	cells := make([]*int, 2)
	i := 1
	cells[i] = new(int)
	*cells[i] = 41
	emit(len(tables["a"]), len(tables["b"]), len(tables["c"]), cells[0] == nil, *cells[1]+1)
}

func faults() {
	recovered("value index out of range", func() {
		rows := make([][]int, 2)
		i := 1
		j := 5
		rows[i] = append(rows[j], 1)
		emit("unreachable", rows)
	})
	recovered("target index out of range", func() {
		rows := make([][]int, 2)
		i := 7
		rows[i] = append(rows[0], 1)
		emit("unreachable", rows)
	})
	recovered("nil map target", func() {
		var m map[string][]int
		k := "k"
		m[k] = append(m[k], 1)
		emit("unreachable", m)
	})
	recovered("nil receiver", func() {
		var h *holder
		h.names = append(h.names, "n")
		emit("unreachable", h)
	})
	recovered("negative make", func() {
		m := map[string][]int{}
		k := "k"
		n := -1
		m[k] = make([]int, n)
		emit("unreachable", m)
	})
}

func shadowedAppend() {
	m := map[string][]int{}
	k := "first"
	append := func(xs []int, x int) []int {
		k = "second"
		return []int{x, len(xs)}
	}
	m[k] = append(m[k], 7)
	emit(len(m), m["first"], m["second"], k)
}

func sideEffectInValue() {
	m := map[string][]int{}
	k := "before"
	rename := func() int {
		k = "after"
		return 3
	}
	m[k] = append(m[k], rename())
	emit(len(m), m["before"], m["after"], k)
}

func run() string {
	lines = nil
	printMap(fillMapOfSlices([]string{"the", "cat", "saw", "the", "dog", "saw", "the", "cat"}))
	sliceOfSlices()
	countsAndBounds()
	throughPointers()
	madeAndNew()
	faults()
	shadowedAppend()
	sideEffectInValue()
	return strings.Join(lines, "\n")
}
