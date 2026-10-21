package main

import (
	"fmt"
	"math"
)

type motion struct {
	vx, vy float64
}

type body struct {
	motion
	x, y   float64
	mass   float64
	single float32
	hits   int
	narrow int32
	tiny   int8
	wide   uint64
	label  string
}

type bodies []body

type world struct {
	members []body
	grid    [][]body
}

var system []body

var trail []string

func reset() {
	system = []body{
		{x: 1, y: 2, mass: 3, motion: motion{vx: 0.5, vy: -0.5}},
		{x: 4, y: 5, mass: 6, motion: motion{vx: 1.5, vy: -1.5}},
		{x: 7, y: 8, mass: 9, motion: motion{vx: 2.5, vy: -2.5}},
	}
	trail = nil
}

func loud(name string, value float64) float64 {
	trail = append(trail, name)
	return value
}

func pick(name string, index int) int {
	trail = append(trail, name)
	return index
}

func advance(step float64) {
	for i := range system {
		system[i].x += step * system[i].vx
		system[i].y -= step * system[i].vy
		system[i].vx *= 2
		system[i].vy /= 4
		system[i].mass = system[i].x + system[i].y
		system[i].single = float32(step) / 3
		system[i].hits += i + 1
		system[i].hits *= 3
		system[i].narrow += math.MaxInt32
		system[i].narrow += int32(i + 2)
		system[i].tiny -= 100
		system[i].tiny -= 100
		system[i].wide += 7
		system[i].label += "!"
	}
}

func integerOperators(values []body, index int) {
	values[index].hits = 1000
	values[index].hits -= 7
	values[index].hits /= 3
	values[index].hits %= 100
	values[index].hits |= 0x100
	values[index].hits &= 0x1F3
	values[index].hits ^= 0xFF
	values[index].hits &^= 0x3
	values[index].hits <<= 2
	values[index].hits >>= 1
	values[index].hits++
	values[index].hits--
	values[index].narrow = -5
	values[index].narrow *= -3
	values[index].tiny = 'a'
}

func bump(values []body, index int, by float64) {
	values[index].x += by
}

func bumpAll(values []body, by float64) {
	for i := 0; i < len(values); i++ {
		bump(values, i, by)
	}
}

func named(values bodies, index uint8, hits int) {
	values[index].hits = hits
	values[index].vx = float64(hits)
}

func inWorld(w *world, row, column int) {
	w.members[column].x = 10
	w.members[column].x -= 0.5
	w.grid[row][column].hits = 4
	w.grid[row][column].hits -= 9
	w.grid[row][column].y = math.Inf(-1)
}

func caught(action func()) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = fmt.Sprint(r)
		}
	}()
	action()
	return "done"
}

func failures() string {
	var none []body
	short := []float64{1, 2}
	zero := 0
	return fmt.Sprint(
		caught(func() { system[3].x = 1 }), "; ",
		caught(func() { system[-1+zero].hits = 1 }), "; ",
		caught(func() { none[0].x = 1 }), "; ",
		caught(func() { none[0].hits += 1 }), "; ",
		caught(func() { system[5].x -= 1 }), "; ",
		caught(func() { system[1].x -= short[2+zero] }), "; ",
		caught(func() { system[4].x -= short[2+zero] }), "; ",
		caught(func() { system[1].hits /= zero }), "; ",
		caught(func() { system[6].hits /= zero }), "; ",
		caught(func() { system[pick("index", 2)].x = loud("value", 1.5) }), "; ",
		caught(func() { system[pick("compound index", 2)].x -= loud("operand", 0.25) }), "; ",
		caught(func() { system[pick("alone", 0)].hits = 44 }), "; ",
		caught(func() { system[pick("compound alone", 0)].hits -= 4 }), "; ",
		system[1].x, " ", system[1].hits, " ", system[2].x, " ", system[0].hits, " ", trail)
}

func aliases() string {
	backing := make([]body, 4)
	front, back := backing[:2], backing[1:]
	front[1].x = 3
	back[0].x *= 2
	back[0].hits = 5
	front[1].hits -= 2
	kept := backing[1]
	backing[1].x = -1
	backing[1].hits = -1
	return fmt.Sprint(backing[1].x, backing[1].hits, kept.x, kept.hits, front[1].x, back[0].hits)
}

func specials() string {
	values := make([]body, 2)
	values[0].x = math.NaN()
	values[0].y = math.Copysign(0, -1)
	values[1].x = math.Inf(1)
	values[1].x -= math.Inf(1)
	values[1].single = 16777217
	values[1].y = 1
	values[1].y /= 3
	return fmt.Sprint(values[0].x, 1/values[0].y, values[1].x, values[1].single, values[1].y)
}

func run() string {
	reset()
	advance(0.5)
	advance(2)
	first := fmt.Sprintf("%+v", system)
	integerOperators(system, 2)
	bumpAll(system, 0.25)
	named(system, 1, -8)
	w := &world{members: system, grid: [][]body{make([]body, 1), make([]body, 3)}}
	inWorld(w, 1, 2)
	return fmt.Sprint(first, "\n", fmt.Sprintf("%+v", system), "\n", fmt.Sprintf("%+v", w.grid), "\n",
		failures(), "\n", aliases(), "\n", specials())
}
