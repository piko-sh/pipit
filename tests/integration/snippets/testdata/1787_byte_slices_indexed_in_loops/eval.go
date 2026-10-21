package main

import (
	"fmt"
	"strings"
)

type shade uint8

type frame struct {
	pixels []byte
	rows   [][]byte
}

var screen []byte
var palette []byte
var shades []shade
var fixed [8]byte
var none []byte

func caught(body func()) (message string) {
	defer func() {
		if r := recover(); r != nil {
			message = fmt.Sprint(r)
		}
	}()
	body()
	return "no panic"
}

func remap(source []byte) int {
	total := 0
	for i := 0; i < len(source); i++ {
		screen[i] = palette[source[i]]
		total += int(screen[i])
	}
	return total
}

func growing(n int) []byte {
	out := []byte{}
	for i := 0; i < n; i++ {
		out = append(out, byte(i))
		out[i] += out[i/2]
	}
	return out
}

func replace() {
	screen = make([]byte, 4)
}

func swappedInside(n int) string {
	seen := []string{}
	for i := 0; i < n; i++ {
		screen[i] = byte(10 + i)
		if i == 1 {
			replace()
		}
		seen = append(seen, fmt.Sprint(screen[i]))
	}
	return strings.Join(seen, ",")
}

func aliased() string {
	first := make([]byte, 6)
	second := first[2:]
	for i := range second {
		second[i] = byte(i + 1)
		first[i] += second[i] * 2
	}
	return fmt.Sprint(first, second)
}

func named(n int) int {
	total := 0
	for i := 0; i < n; i++ {
		shades[i] = shade(i * 3)
		total += int(shades[i])
	}
	return total
}

func arrays() int {
	total := 0
	for i := range fixed {
		fixed[i] = byte(i * i)
		total += int(fixed[i])
	}
	return total
}

func nested(f *frame) int {
	total := 0
	for y := range f.rows {
		row := f.rows[y]
		for x := range row {
			row[x] = byte(x + y)
			f.pixels[y*len(row)+x] = row[x] + 1
			total += int(f.pixels[y*len(row)+x])
		}
	}
	return total
}

func pastTheEnd() {
	for i := 0; i < 6; i++ {
		screen[i] = palette[i]
	}
}

func readNil(n int) int {
	total := 0
	for i := 0; i < n; i++ {
		total += int(none[i])
	}
	return total
}

func run() string {
	palette = make([]byte, 256)
	for i := range palette {
		palette[i] = byte(255 - i)
	}
	screen = make([]byte, 5)
	shades = make([]shade, 5)
	f := &frame{pixels: make([]byte, 6), rows: [][]byte{make([]byte, 3), make([]byte, 3)}}

	lines := []string{
		fmt.Sprint("remap ", remap([]byte{0, 1, 250, 7, 9}), screen),
		fmt.Sprint("growing ", growing(6)),
	}
	screen = make([]byte, 5)
	lines = append(lines,
		"swapped "+swappedInside(4)+fmt.Sprint(screen),
		"aliased "+aliased(),
		fmt.Sprint("named ", named(5), shades),
		fmt.Sprint("arrays ", arrays(), fixed),
		fmt.Sprint("nested ", nested(f), f.pixels, f.rows),
	)
	screen = make([]byte, 5)
	lines = append(lines,
		"past the end "+caught(pastTheEnd)+fmt.Sprint(screen),
		fmt.Sprint("nil with no trips ", readNil(0)),
		"nil with one trip "+caught(func() { readNil(1) }),
	)
	return strings.Join(lines, "\n")
}
