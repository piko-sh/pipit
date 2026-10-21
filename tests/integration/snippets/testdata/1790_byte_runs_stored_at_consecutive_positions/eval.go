package main

import "fmt"

type pixel struct{ r, g, b uint8 }

var palette = []pixel{{1, 2, 3}, {40, 50, 60}, {250, 251, 252}}

func caught(body func()) (message string) {
	defer func() {
		if r := recover(); r != nil {
			message = fmt.Sprint(r)
		}
	}()
	body()
	return "no panic"
}

func blit(out []byte, source []byte) {
	for i := 0; i < len(source); i++ {
		colour := palette[source[i]]
		position := i * 4
		out[position] = colour.b
		out[position+1] = colour.g
		out[position+2] = colour.r
		out[position+3] = 255
	}
}

func pairs(out []byte, n int) {
	for i := 0; i < n; i++ {
		out[2*i] = byte(i)
		out[2*i+1] = byte(i * 3)
	}
}

func triples(out []byte, n int) {
	for i := 0; i < n; i++ {
		base := i * 3
		out[base+1] = byte(i + 100)
		out[base+2] = byte(i + 200)
		out[base+3] = byte(i + 300)
	}
}

func five(out []byte, at int, value uint64) {
	out[at] = byte(value)
	out[at+1] = byte(value >> 8)
	out[at+2] = byte(value >> 16)
	out[at+3] = byte(value >> 24)
	out[at+4] = byte(value >> 32)
}

func twoSlices(first, second []byte, at int) {
	first[at] = 1
	second[at+1] = 2
	first[at+2] = 3
	second[at+3] = 4
}

func backwards(out []byte, at int) {
	out[at+3] = 4
	out[at+2] = 3
	out[at+1] = 2
	out[at] = 1
}

func sameValue(out []byte, at int, value byte) {
	out[at] = value
	out[at+1] = value
	out[at+2] = value
	out[at+3] = value
}

func storeTwo(out []byte, at int, a, b byte) {
	out[at+1] = a
	out[at+2] = b
}

func storeThree(out []byte, at int, a, b, c byte) {
	out[at] = a
	out[at+1] = b
	out[at+2] = c
}

func storeFour(out []byte, at int, a, b, c, d byte) {
	out[at] = d
	out[at+1] = c
	out[at+2] = b
	out[at+3] = a
}

func storeSix(out []byte, at int, a, b, c, d, e, f byte) {
	out[at] = a
	out[at+1] = b
	out[at+2] = c
	out[at+3] = d
	out[at+4] = e
	out[at+5] = f
}

func farAlong(out []byte, at int, a, b byte) {
	out[at+254] = a
	out[at+255] = b
}

func branchInside(out []byte, at int, skip bool) {
	out[at] = 9
	if !skip {
		out[at+1] = 8
	}
	out[at+2] = 7
	out[at+3] = 6
}

func run() string {
	screen := make([]byte, 12)
	blit(screen, []byte{0, 2, 1})
	lines := []string{fmt.Sprint(screen)}

	two := make([]byte, 8)
	pairs(two, 4)
	lines = append(lines, fmt.Sprint(two))

	three := make([]byte, 10)
	triples(three, 3)
	lines = append(lines, fmt.Sprint(three))

	wide := make([]byte, 6)
	five(wide, 1, 0x0102030405)
	lines = append(lines, fmt.Sprint(wide))

	for _, length := range []int{0, 1, 2, 3, 4} {
		short := make([]byte, length)
		message := caught(func() { sameValue(short, 0, 7) })
		lines = append(lines, fmt.Sprint(length, " ", message, " ", short))
	}
	edge := make([]byte, 6)
	lines = append(lines, caught(func() { sameValue(edge, 2, 5) }), fmt.Sprint(edge))
	lines = append(lines, caught(func() { sameValue(edge, 3, 6) }), fmt.Sprint(edge))
	lines = append(lines, caught(func() { sameValue(edge, -1, 8) }), fmt.Sprint(edge))
	lines = append(lines, caught(func() { sameValue(edge, -3, 8) }), fmt.Sprint(edge))
	lines = append(lines, caught(func() { sameValue(nil, 0, 8) }))
	lines = append(lines, caught(func() { blit(make([]byte, 7), []byte{1, 2}) }))

	for length := 0; length <= 7; length++ {
		short := make([]byte, length)
		message := caught(func() { storeTwo(short, 0, 21, 22) })
		lines = append(lines, fmt.Sprint("two ", length, " ", message, " ", short))
		short = make([]byte, length)
		message = caught(func() { storeThree(short, 1, 31, 32, 33) })
		lines = append(lines, fmt.Sprint("three ", length, " ", message, " ", short))
		short = make([]byte, length)
		message = caught(func() { storeFour(short, length-4, 41, 42, 43, 44) })
		lines = append(lines, fmt.Sprint("four ", length, " ", message, " ", short))
		short = make([]byte, length)
		message = caught(func() { storeSix(short, 0, 61, 62, 63, 64, 65, 66) })
		lines = append(lines, fmt.Sprint("six ", length, " ", message, " ", short))
	}
	long := make([]byte, 300)
	farAlong(long, 40, 7, 8)
	lines = append(lines, fmt.Sprint(long[292:298]), caught(func() { farAlong(long, 45, 9, 10) }), fmt.Sprint(long[296:]))
	lines = append(lines, caught(func() { farAlong(long, -255, 11, 12) }), fmt.Sprint(long[:2]))
	lines = append(lines, caught(func() { farAlong(long, -254, 13, 14) }), fmt.Sprint(long[:2]))

	first, second := make([]byte, 4), make([]byte, 4)
	twoSlices(first, second, 0)
	lines = append(lines, fmt.Sprint(first, second))
	twoSlices(first, first, 0)
	lines = append(lines, fmt.Sprint(first))

	back := make([]byte, 5)
	backwards(back, 1)
	lines = append(lines, fmt.Sprint(back), caught(func() { backwards(back, 2) }), fmt.Sprint(back))

	branched := make([]byte, 4)
	branchInside(branched, 0, true)
	lines = append(lines, fmt.Sprint(branched))
	branchInside(branched, 0, false)
	lines = append(lines, fmt.Sprint(branched))

	out := ""
	for _, line := range lines {
		out += line + "\n"
	}
	return out
}
