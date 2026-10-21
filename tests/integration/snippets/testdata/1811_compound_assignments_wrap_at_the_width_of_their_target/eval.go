package main

import "fmt"

type box struct {
	small uint8
	mid   int16
	wide  uint32
	inner struct{ tiny int8 }
}

var (
	g8  uint8  = 250
	g16 uint16 = 65000
	g32 uint32 = 4000000000
	i8  int8   = 120
	i16 int16  = 32000
	i32 int32  = 2000000000
	gb  box
	ga  [3]uint8
	gs  = []int16{32000}
	gm  = map[string]uint8{"k": 250}
	gp  = &box{small: 250}
)

func big(n int) bool { return n > 70000 || n < -70000 }

func globals() string {
	g8 += 10
	g16 += 1000
	g32 += 400000000
	i8 += 10
	i16 += 1000
	i32 += 400000000
	g8 <<= 1
	g16 *= 3
	i8 -= 100
	i8 -= 100
	g32 -= 3000000000
	g32 -= 3000000000
	return fmt.Sprint(g8, g16, g32, i8, i16, i32, " ", g8 > 100, g16 > 60000, g32 > 1<<31, i8 > 0, i16 < 0, i32 < 0, uint64(g16)+1, int(i8)+1)
}

func aggregates() string {
	gb.small = 250
	gb.small += 10
	gb.mid = 32000
	gb.mid += 1000
	gb.wide = 4000000000
	gb.wide += 400000000
	gb.inner.tiny = 120
	gb.inner.tiny += 10
	ga[1] = 250
	ga[1] += 10
	gs[0] += 1000
	gm["k"] += 10
	gp.small += 10
	return fmt.Sprint(gb, ga, gs, gm, gp.small, " ", gb.small > 100, gb.mid > 0, gb.wide > 1<<31, gb.inner.tiny > 0, ga[1] > 100, gs[0] > 0, gm["k"] > 100, gp.small > 100,
		int(gb.small)+1, int(gb.mid)+1, uint64(gb.wide)+1, int(gb.inner.tiny)+1)
}

func captured() string {
	var c8 uint8 = 250
	var c16 int16 = 32000
	var c32 uint32 = 4000000000
	bump := func() {
		c8 += 10
		c16 += 1000
		c32 += 400000000
	}
	bump()
	return fmt.Sprint(c8, c16, c32, " ", c8 > 100, c16 > 0, c32 > 1<<31, int(c8)+1, int(c16)+1, uint64(c32)+1)
}

var gf float32 = 16777216

func floats() string {
	gf += 1
	var lf float32 = 16777216
	lf += 1
	fs := []float32{16777216}
	fs[0] += 1
	return fmt.Sprint(float64(gf) == 16777217, float64(lf) == 16777217, float64(fs[0]) == 16777217, gf, lf, fs[0])
}

func counter() (func() bool, func() int) {
	var n uint8 = 250
	var m int16 = 32000
	return func() bool {
			n += 10
			m += 1000
			return n > 100 || m > 0
		}, func() int {
			return int(n)*100000 + int(m)
		}
}

func nested() string {
	var deep uint32 = 4000000000
	outer := func() func() bool {
		return func() bool {
			deep += 400000000
			deep *= 3
			return deep > 1<<31
		}
	}
	over := outer()()
	return fmt.Sprint(over, deep, uint64(deep)+1)
}

func escaping() string {
	step, read := counter()
	first := step()
	return fmt.Sprint(first, read())
}

func pointers() string {
	var a uint8 = 250
	var b int16 = 32000
	var c uint32 = 4000000000
	pa, pb, pc := &a, &b, &c
	*pa += 10
	*pb += 1000
	*pc += 400000000
	*pa++
	*pb--
	return fmt.Sprint(a, b, c, " ", a > 100, b > 0, c > 1<<31, int(a)+1, int(b)+1, uint64(c)+1)
}

func locals() string {
	var a uint8 = 250
	var b int16 = 32000
	var c uint32 = 4000000000
	var d int32 = 2000000000
	a += 10
	b += 1000
	c += 400000000
	d += 400000000
	a++
	b--
	return fmt.Sprint(a, b, c, d, " ", a > 100, b > 0, c > 1<<31, d > 0, int(a)+1, int(b)+1, uint64(c)+1, int64(d)+1)
}

type cell uint8

func elements() string {
	bytes := []byte{250, 1, 128}
	shorts := []int16{32000, -32000}
	words := []uint32{4000000000, 7}
	smalls := [4]int8{120, -120, 1, 64}
	named := []cell{200, 100}
	for i := 0; i < len(bytes); i++ {
		bytes[i] += 10
		bytes[i] ^= 0xF0
		bytes[i] <<= 1
	}
	for i := range shorts {
		shorts[i] *= 3
		shorts[i] -= 20000
	}
	words[0] += 400000000
	words[1] *= 1 << 30
	words[1] <<= 4
	pointer := &smalls
	for i := 0; i < len(pointer); i++ {
		pointer[i] += 100
		pointer[i] *= 2
	}
	named[0] += 100
	named[1] -= 101
	return fmt.Sprint(bytes, shorts, words, smalls, named, " ",
		bytes[0] > 100, shorts[0] > 0, words[0] > 1<<31, smalls[0] > 0, named[0] > 100,
		int(bytes[2])+1, int(shorts[1])+1, uint64(words[1])+1, int(smalls[3])+1, int(named[1])+1)
}

func bumpAll[T ~uint8 | ~int16 | ~uint32](values []T, by T) (total uint64) {
	for i := 0; i < len(values); i++ {
		values[i] += by
		values[i] *= 3
		total += uint64(values[i])
	}
	return total
}

func generics() string {
	a := []uint8{80, 90}
	b := []int16{10000, 11000}
	c := []uint32{1400000000, 5}
	d := []cell{85}
	return fmt.Sprint(bumpAll(a, 10), bumpAll(b, 1000), bumpAll(c, 100000000), bumpAll(d, 1), a, b, c, d)
}

func run() string {
	return fmt.Sprint(globals(), "\n", aggregates(), "\n", captured(), "\n", floats(), "\n", escaping(), "\n",
		nested(), "\n", pointers(), "\n", locals(), "\n", elements(), "\n", generics())
}
