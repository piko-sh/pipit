package main

import (
	"fmt"
	"strings"
)

type frame struct {
	pix []byte
}

func caught(body func()) (message string) {
	defer func() {
		if r := recover(); r != nil {
			message = fmt.Sprint(r)
		}
	}()
	body()
	return "no panic"
}

func fillPixels(buffer []byte, palette [][3]byte, indices []byte) {
	for i := 0; i < len(indices); i++ {
		colour := palette[indices[i]%byte(len(palette))]
		position := i * 4
		buffer[position] = colour[0]
		buffer[position+1] = colour[1]
		buffer[position+2] = colour[2]
		buffer[position+3] = 0xff
	}
}

func throughField(target *frame, start int, value uint64) {
	target.pix[start+1] = byte(value)
	target.pix[start+2] = byte(value >> 8)
	target.pix[start+3] = byte(value * 3)
}

func farOffsets(buffer []byte, base int) {
	buffer[base+255] = 7
	buffer[base+256] = 8
	buffer[base+254] = 9
}

func sumKept(buffer []byte, base int) int {
	next := base + 1
	buffer[next] = 5
	return next * 10
}

func bumpedInPlace(buffer []byte, at int) int {
	at = at + 2
	buffer[at] = 6
	at = 40
	return at
}

func checksum(buffer []byte) int {
	total := 0
	for i, b := range buffer {
		total += (i + 1) * int(b)
	}
	return total
}

func run() string {
	var lines []string
	emit := func(values ...any) { lines = append(lines, fmt.Sprint(values...)) }

	pixels := make([]byte, 6*4)
	fillPixels(pixels, [][3]byte{{1, 2, 3}, {40, 50, 60}, {250, 251, 252}}, []byte{0, 1, 2, 2, 1, 0})
	emit(pixels, checksum(pixels))

	held := &frame{pix: make([]byte, 8)}
	throughField(held, 0, 0x1ff)
	throughField(held, 4, 300)
	emit(held.pix)

	wide := make([]byte, 520)
	farOffsets(wide, 3)
	farOffsets(wide, 263)
	emit(wide[257], wide[258], wide[259], wide[517], wide[518], wide[519], checksum(wide))

	small := make([]byte, 4)
	emit(sumKept(small, 1), bumpedInPlace(small, 1), small)

	emit(caught(func() { fillPixels(make([]byte, 7), [][3]byte{{1, 2, 3}}, []byte{0, 0}) }))
	emit(caught(func() { throughField(&frame{pix: make([]byte, 3)}, 0, 9) }))
	emit(caught(func() { throughField(&frame{pix: make([]byte, 3)}, -1, 9) }))
	emit(caught(func() { throughField(&frame{pix: make([]byte, 3)}, -2, 9) }))
	emit(caught(func() { farOffsets(make([]byte, 258), 3) }))
	emit(caught(func() { farOffsets(make([]byte, 259), 3) }))
	emit(caught(func() { sumKept(nil, 0) }))
	last := make([]byte, 4)
	emit(caught(func() { throughField(&frame{pix: last}, 0, 77) }), last)
	return strings.Join(lines, "\n")
}
