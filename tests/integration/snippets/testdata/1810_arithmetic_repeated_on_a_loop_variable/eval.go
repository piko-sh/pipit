package main

import "fmt"

func distance(left, right string) int {
	row := make([]int, len(right)+1)
	next := make([]int, len(right)+1)
	for j := 0; j <= len(right); j++ {
		row[j] = j
	}
	for i := 1; i <= len(left); i++ {
		next[0] = i
		for j := 1; j <= len(right); j++ {
			cost := 1
			if left[i-1] == right[j-1] {
				cost = 0
			}
			best := row[j] + 1
			if next[j-1]+1 < best {
				best = next[j-1] + 1
			}
			if row[j-1]+cost < best {
				best = row[j-1] + cost
			}
			next[j] = best
		}
		row, next = next, row
	}
	return row[len(right)]
}

func stencil(cells []int, width int) int {
	sum := 0
	for i := width + 1; i < len(cells)-width-1; i++ {
		sum += cells[i-width-1] + cells[i-width] + cells[i-width+1]
		sum -= cells[i+width-1] + cells[i+width] + cells[i+width+1]
		sum += cells[i-1] * cells[i+1]
	}
	return sum
}

func operandChangesBetween(values []int) (first, second int) {
	i := 2
	first = values[i-1]
	i += 2
	second = values[i-1]
	return first, second
}

func operandChangesOnOneBranch(values []int, flip bool) int {
	i := 3
	before := values[i-1]
	if flip {
		i = 1
	}
	return before*10 + values[i-1]
}

func resultReadAfterTheFirstIsOverwritten(a, b int) int {
	first := a + b
	keep := first
	first = first * 2
	again := a + b
	return keep + first + again
}

func parameters(a, b uint64, c, d float64) (uint64, float64) {
	x := a*b + 7
	y := a*b + 9
	p := c/d - 1
	q := c/d + 1
	return x ^ y, p * q
}

func faultsOnce(divisor int) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = fmt.Sprint(r)
		}
	}()
	total := 100 / divisor
	total += 100 / divisor
	return fmt.Sprint(total)
}

func inALoopThatCalls(values []int, step func(int) int) int {
	total := 0
	for i := 1; i < len(values); i++ {
		total += values[i-1]
		i = step(i)
		total += values[i-1]
	}
	return total
}

func run() string {
	cells := make([]int, 40)
	for i := range cells {
		cells[i] = (i*7 + 3) % 11
	}
	values := []int{10, 20, 30, 40, 50, 60}
	skip := func(i int) int {
		if i%2 == 1 {
			return i + 1
		}
		return i
	}
	return fmt.Sprint(
		distance("kitten", "sitting"), distance("", "abc"), distance("flaw", "lawn"), distance("same", "same"), " ",
		stencil(cells, 5), stencil(cells, 1), " ",
		fmt.Sprint(operandChangesBetween(values)), operandChangesOnOneBranch(values, false), operandChangesOnOneBranch(values, true), " ",
		resultReadAfterTheFirstIsOverwritten(3, 4), fmt.Sprint(parameters(1<<40, 1<<30, 7, 2)), " ",
		faultsOnce(7), faultsOnce(0), " ",
		inALoopThatCalls(values, skip))
}
