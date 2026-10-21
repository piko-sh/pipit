package main

import "fmt"

type half int32

func store32(p *[]int32, i int, v int32) { (*p)[i] = v }

func load32(p *[]int32, i int) int32 { return (*p)[i] }

func store16(p *[]int16, i int, v int16) { (*p)[i] = v }

func store8(p *[]int8, i int, v int8) { (*p)[i] = v }

func load8(p *[]int8, i int) int8 { return (*p)[i] }

func storeRune(p *[]rune, i int, v rune) { (*p)[i] = v }

func storeHalf(p *[]half, i int, v half) { (*p)[i] = v }

func store64(p *[]int64, i int, v int64) { (*p)[i] = v }

func storeInt(p *[]int, i int, v int) { (*p)[i] = v }

func swap16(p *[]int16, i, j int) { (*p)[i], (*p)[j] = (*p)[j], (*p)[i] }

func caught(body func()) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = fmt.Sprint(r)
		}
	}()
	body()
	return "ok"
}

func run() string {
	a := []int32{1, 2, 3, 4, 5, 6, 7, 8}
	b := []int16{1, 2, 3, 4, 5, 6, 7, 8}
	c := []int8{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	d := []rune("abcdefgh")
	e := []half{1, 2, 3, 4}
	f := []int64{1, 2, 3, 4}
	g := []int{1, 2, 3, 4}
	store32(&a, 1, -70)
	store32(&a, 7, 1<<30)
	store16(&b, 1, -7)
	store8(&c, 1, -50)
	store8(&c, 15, 127)
	storeRune(&d, 1, 'X')
	storeHalf(&e, 3, 9)
	store64(&f, 1, -1<<40)
	storeInt(&g, 2, 1<<40)
	swap16(&b, 0, 7)
	return fmt.Sprint(
		a, load32(&a, 1), load32(&a, 7), " ",
		b, " ",
		c, load8(&c, 1), load8(&c, 15), " ",
		string(d), " ", e, " ", f, " ", g, " ",
		caught(func() { store32(&a, 8, 1) }), " ",
		caught(func() { _ = load8(&c, -1) }),
	)
}
