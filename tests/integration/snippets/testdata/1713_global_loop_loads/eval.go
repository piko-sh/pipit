package main

import "fmt"

type image struct{ pixels []byte }

var screen *image
var source = []uint{2, 3, 5, 7}
var replacement = []uint{11, 13, 17, 19}

func rebind() { source = replacement }

func reboundLoop() int {
	total := 0
	for i := 0; i < 4; i++ {
		total += int(source[i])
		if i == 1 {
			rebind()
		}
	}
	return total
}

func paint(n int) int {
	total := 0
	for y := 0; y < n; y++ {
		for x := 0; x < 4; x++ {
			screen.pixels[x] = byte(source[x]) + byte(y)
			total += int(screen.pixels[x])
		}
	}
	return total
}

func replace() { source = []uint{11, 13, 17, 19}; screen = &image{make([]byte, 4)} }

func callbacks() int {
	total := 0
	for i := 0; i < 4; i++ {
		total += int(source[i])
		if i == 1 {
			replace()
		}
	}
	return total
}

func panicOrder(n int) (result string) {
	defer func() {
		if p := recover(); p != nil {
			result = fmt.Sprint(p)
		}
	}()
	for i := 0; i < n; i++ {
		value := source[100+i]
		screen.pixels[i] = byte(value)
	}
	return "none"
}

func run() string {
	zero := paint(0)
	order := panicOrder(1)
	screen = &image{make([]byte, 4)}
	first := paint(3)
	alias := screen.pixels
	alias[2] = 29
	second := callbacks()
	third := paint(1)
	source = []uint{2, 3, 5, 7}
	fourth := reboundLoop()
	return fmt.Sprint(zero, " ", first, " ", second, " ", third, " ", fourth, " ", alias, " ", order)
}
