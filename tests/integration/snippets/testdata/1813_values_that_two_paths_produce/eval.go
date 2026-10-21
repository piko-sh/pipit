package main

import "fmt"

type screen struct {
	pixels []byte
	width  int
}

func at(data []byte, index int) byte {
	if index < 0 || index >= len(data) {
		return 0
	}
	return data[index]
}

func pick(values []uint32, index int) uint32 {
	if index >= len(values) {
		return 7
	}
	return values[index]
}

func grade(score int) uint64 {
	if score < 10 {
		return 1
	}
	if score < 20 {
		return 2
	}
	return uint64(score)
}

func draw(target *screen, palette, source []byte, start, step, count int) (sum int) {
	position := start
	for count > 0 {
		shade := at(source, position>>4&127)
		target.pixels[count%len(target.pixels)] = palette[int(shade)]
		sum += int(at(source, position))
		position += step
		count--
	}
	return sum
}

func total(values []uint32, limit int) (sum int, last uint32) {
	for i := 0; i < limit; i++ {
		value := pick(values, i)
		sum += int(value)
		last = value
	}
	return sum, last
}

func graded(scores []int) (sum int, kept uint64) {
	for _, score := range scores {
		g := grade(score)
		sum += int(g)
		if g > kept {
			kept = g
		}
	}
	return sum, kept
}

func chosen(flag bool, low, high uint8) int {
	var value uint8
	if flag {
		value = low
	} else {
		value = high
	}
	return int(value) * 3
}

func run() string {
	palette := make([]byte, 256)
	for i := range palette {
		palette[i] = byte(255 - i)
	}
	source := make([]byte, 100)
	for i := range source {
		source[i] = byte(i*7 + 3)
	}
	target := &screen{pixels: make([]byte, 16), width: 4}
	first := draw(target, palette, source, 0, 37, 40)
	second := draw(target, palette, source, -64, 300, 25)
	sum, last := total([]uint32{4000000000, 5, 6}, 6)
	gradedSum, kept := graded([]int{3, 12, 25, 19, 40, 9})
	return fmt.Sprint(first, second, target.pixels, " ", sum, last, " ", gradedSum, kept, " ",
		chosen(true, 200, 9), chosen(false, 200, 9), int(at(nil, 0)), int(at(source, 99)), int(pick(nil, 0)))
}
