package main

import "fmt"

func run() string {
	var out []string
	add := func(label string, values ...any) { out = append(out, label+"="+fmt.Sprint(values...)) }
	panics := func(label string, slice func()) {
		defer func() { add(label, recover() != nil) }()
		slice()
	}

	ints := make([]int, 4, 6)
	for i := range ints {
		ints[i] = i + 1
	}
	low, high := 1, 3
	a := ints[low:high]
	b := ints[low:]
	c := ints[:high]
	d := ints[:]
	add("ints", a, len(a), cap(a), b, len(b), cap(b), c, len(c), cap(c), d, len(d), cap(d))
	atLen := ints[len(ints):]
	beyond := ints[2:6]
	beyond[3] = 7
	add("intsEdges", len(atLen), cap(atLen), beyond, ints[:6])
	atCap := ints[cap(ints):cap(ints)]
	atCap = append(atCap, 99)
	add("intsAtCap", atCap, len(atCap), ints[:6])
	shrink := ints
	steps := 0
	for len(shrink) > 0 {
		shrink = shrink[1:]
		steps++
	}
	add("intsShrink", steps, len(shrink), cap(shrink))
	a[0] = 42
	add("intsAlias", ints, b[0])

	floats := make([]float64, 3, 5)
	for i := range floats {
		floats[i] = float64(i) + 0.5
	}
	ff := floats[1:]
	add("floats", ff, cap(ff), floats[3:5], floats[5:5], len(floats[3:]))

	words := make([]string, 3, 4)
	words[0], words[1], words[2] = "a", "b", "c"
	ww := words[:2]
	ww = append(ww, "x")
	add("strings", words, ww, words[4:4], words[1:4])

	flags := make([]bool, 2, 3)
	flags[1] = true
	add("bools", flags[1:], flags[:3], len(flags[2:]), cap(flags[3:3]))

	counts := make([]uint64, 2, 2)
	counts[0], counts[1] = 7, 8
	add("uints", counts[1:], counts[2:], cap(counts[2:]))

	raw := make([]byte, 3, 8)
	copy(raw, "abc")
	add("bytes", string(raw[1:]), string(raw[:2]), len(raw[8:8]), cap(raw[3:]))

	seven, three, two, minus := 7, 3, 2, -1
	panics("highAboveCap", func() { _ = ints[:seven] })
	panics("lowAboveHigh", func() { _ = ints[three:two] })
	panics("negativeLow", func() { _ = ints[minus:] })
	panics("bytesHighAboveCap", func() { _ = raw[:seven+2] })
	panics("withinBounds", func() { _ = ints[two:three] })

	return fmt.Sprint(out)
}
