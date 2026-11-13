package main

import "fmt"

type sample struct {
	weight int32
	label  string
}

type holder struct {
	items []uint32
}

var queue []int32

func total(values []uint32) uint32 {
	var sum uint32
	for i := 0; i < len(values); i++ {
		sum += values[i]
	}
	return sum
}

func heaviest(samples []sample) int32 {
	best := int32(-1)
	for i := 0; i < len(samples); i++ {
		if samples[i].weight > best {
			best = samples[i].weight
		}
	}
	return best
}

func sumOf[T int16 | uint32 | float32](values []T) T {
	var sum T
	for i := 0; i < len(values); i++ {
		sum += values[i]
	}
	return sum
}

func grid(rows [][]int16) int {
	count := 0
	for r := 0; r < len(rows); r++ {
		for c := 0; c < len(rows[r]); c++ {
			count += int(rows[r][c])
		}
	}
	return count
}

func growsAsItGoes() (int, int32) {
	values := []int32{1, 2, 3}
	visits := 0
	var sum int32
	for i := 0; i < len(values); i++ {
		sum += values[i]
		visits++
		if values[i] < 3 {
			values = append(values, values[i]+2)
		}
	}
	return visits, sum
}

func shrinksAsItGoes() (int, int32) {
	values := []int32{5, 6, 7, 8, 9, 10}
	visits := 0
	var sum int32
	for i := 0; i < len(values); i++ {
		sum += values[i]
		visits++
		values = values[:len(values)-1]
	}
	return visits, sum
}

func double(value int32) int32 { return 2 * value }

func enqueue(value int32) int32 {
	if value < 4 {
		queue = append(queue, value+1)
	}
	return value
}

func drainsAQueueThatGrows() (int, int32) {
	queue = []int32{1}
	visits := 0
	var sum int32
	for i := 0; i < len(queue); i++ {
		sum += double(enqueue(queue[i]))
		visits++
	}
	return visits, sum
}

func throughAField(h *holder) (int, uint32) {
	visits := 0
	var sum uint32
	for i := 0; i < len(h.items); i++ {
		sum += h.items[i]
		visits++
		if i == 1 {
			h.items = h.items[:3]
		}
	}
	return visits, sum
}

func throughAPointer(values *[]uint32) (int, uint32) {
	visits := 0
	var sum uint32
	for i := 0; i < len(*values); i++ {
		sum += (*values)[i]
		visits++
		if i == 0 {
			*values = append(*values, 100)
		}
	}
	return visits, sum
}

func throughAClosure() (int, uint32) {
	values := []uint32{1, 2}
	grow := func() {
		if len(values) < 5 {
			values = append(values, 10)
		}
	}
	visits := 0
	var sum uint32
	for i := 0; i < len(values); i++ {
		sum += values[i]
		visits++
		grow()
	}
	return visits, sum
}

func aliasKeepsItsOwnLength() (int, uint32) {
	values := []uint32{1, 2, 3, 4}
	view := values
	visits := 0
	var sum uint32
	for i := 0; i < len(view); i++ {
		sum += view[i]
		visits++
		values = values[:1]
	}
	return visits + len(values), sum
}

func fillsAMap() int {
	seen := map[int]int{}
	for len(seen) < 4 {
		seen[len(seen)] = len(seen) * 2
	}
	return len(seen) + seen[3]
}

func fillsAChannel() int {
	pending := make(chan int, 8)
	for len(pending) < 5 {
		pending <- len(pending)
	}
	return len(pending)
}

func arrayAndItsPointer() int {
	fixed := [5]int16{1, 2, 3, 4, 5}
	pointer := &fixed
	sum := 0
	for i := 0; i < len(fixed); i++ {
		sum += int(fixed[i])
	}
	for i := 0; i < len(pointer); i++ {
		sum += int(pointer[i])
	}
	return sum
}

func run() string {
	h := &holder{items: []uint32{1, 2, 3, 4, 5, 6}}
	through := []uint32{7, 8}
	return fmt.Sprint(
		total([]uint32{1, 2, 3, 4000000000}), total(nil), " ",
		heaviest([]sample{{weight: 3}, {weight: 9, label: "x"}, {weight: -4}}), heaviest(nil), " ",
		sumOf([]int16{30000, 30000}), sumOf([]uint32{4000000000, 500000000}), sumOf([]float32{0.5, 0.25}), " ",
		grid([][]int16{{1, 2}, nil, {3}, {4, 5, 6}}), " ",
		fmt.Sprint(growsAsItGoes()), fmt.Sprint(shrinksAsItGoes()), fmt.Sprint(drainsAQueueThatGrows()), " ",
		fmt.Sprint(throughAField(h)), len(h.items), fmt.Sprint(throughAPointer(&through)), through, " ",
		fmt.Sprint(throughAClosure()), fmt.Sprint(aliasKeepsItsOwnLength()), " ",
		fillsAMap(), fillsAChannel(), arrayAndItsPointer())
}
