package main

import (
	"fmt"
	"math"
	"strings"
)

type grid struct {
	cells []int
	width int
}

func at(row, column int) int {
	return row*8 + column
}

func accumulate(values []int) int {
	total := 3
	for i := range values {
		total = total + values[i]*5
	}
	return total
}

func selfScaled(x int) int {
	x = 7 + x*6
	return x
}

func productKept(n int) (int, int) {
	product := n * 3
	sum := product + n
	return product, sum
}

func wraps(big int) int {
	return big*9 + big
}

func doubled(n int) int {
	product := n * 11
	return product + product
}

func fill(g *grid, rows int) int {
	checksum := 0
	for row := 0; row < rows; row++ {
		for column := 0; column < g.width; column++ {
			position := row*g.width + column
			g.cells[position] = row*100 + column
			checksum = checksum*31 + g.cells[row*4+column]
		}
	}
	return checksum
}

func branches(n int) int {
	total := 0
	for i := 0; i < n; i++ {
		scaled := i * 7
		if i%2 == 0 {
			total = scaled + total
			continue
		}
		total = total + scaled
		total -= scaled / 2
	}
	return total
}

func small(i int) int { return i*4 + 1 }

func inlined(n int) int {
	total := 0
	for i := 0; i < n; i++ {
		total += small(i) + small(total&3)
	}
	return total
}

func run() string {
	first, second := productKept(5)
	g := &grid{cells: make([]int, 12), width: 4}
	lines := []string{
		fmt.Sprint("at ", at(0, 0), at(3, 5), at(-2, 1)),
		fmt.Sprint("accumulate ", accumulate([]int{1, 2, 3, -4}), accumulate(nil)),
		fmt.Sprint("self ", selfScaled(0), selfScaled(9), selfScaled(-3)),
		fmt.Sprint("kept ", first, second),
		fmt.Sprint("wraps ", wraps(math.MaxInt64/3), wraps(math.MinInt64), wraps(math.MaxInt64)),
		fmt.Sprint("doubled ", doubled(3)),
		fmt.Sprint("fill ", fill(g, 3), g.cells),
		fmt.Sprint("branches ", branches(9), branches(0)),
		fmt.Sprint("inlined ", inlined(6)),
	}
	return strings.Join(lines, "\n")
}
