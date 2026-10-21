package main

import "fmt"

type shade struct {
	r, g, b, a uint8
	wide       uint16
	count      uint32
}

var palette [4]shade

var strip []shade

func fill() {
	for i := range palette {
		palette[i] = shade{r: uint8(10 + i), g: uint8(100 + i), b: uint8(200 + i), a: 255, wide: uint16(1000 * i), count: uint32(70000 * i)}
	}
	strip = palette[1:3]
}

func blit(source []byte) []byte {
	out := make([]byte, 0, 4*len(source))
	for i := 0; i < len(source); i++ {
		col := palette[source[i]]
		out = append(out, col.r, col.g, col.b, 0xff)
	}
	return out
}

func fromSlice(index int) uint64 {
	entry := strip[index]
	return uint64(entry.wide) + uint64(entry.count) + uint64(entry.a)
}

func throughPointer(table *[4]shade, index int) uint64 {
	entry := table[index]
	return uint64(entry.r) + uint64(entry.g) + uint64(entry.b)
}

func ofPointers(index int) uint64 {
	held := []*shade{&palette[0], &palette[3], nil}
	entry := held[index]
	return uint64(entry.r) + uint64(entry.g)
}

func keepsItsCopy(index int) string {
	entry := palette[index]
	palette[index].r = 99
	first := fmt.Sprint(entry.r, entry.g, entry.b)
	palette[index].r = uint8(10 + index)
	return first
}

func caught(body func() uint64) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = fmt.Sprint(r)
		}
	}()
	return fmt.Sprint(body())
}

func run() string {
	fill()
	var none []shade
	var absent *[4]shade
	return fmt.Sprint(
		blit([]byte{0, 3, 1, 2}), " ",
		fromSlice(0), " ", fromSlice(1), " ",
		throughPointer(&palette, 2), " ",
		ofPointers(1), " ",
		keepsItsCopy(2), " ",
		caught(func() uint64 { return fromSlice(2) }), " | ",
		caught(func() uint64 { return fromSlice(-1) }), " | ",
		caught(func() uint64 { entry := none[0]; return uint64(entry.r) + uint64(entry.g) }), " | ",
		caught(func() uint64 { return throughPointer(absent, 1) }), " | ",
		caught(func() uint64 { return ofPointers(2) }),
	)
}
