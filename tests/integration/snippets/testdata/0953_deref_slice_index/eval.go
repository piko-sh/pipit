package main

import "fmt"

func pushTwice(values *[]int, a, b int) {
	*values = append(*values, a)
	(*values)[len(*values)-1] = a + 1
	*values = append(*values, b)
}

func swap(values *[]int, i, j int) {
	(*values)[i], (*values)[j] = (*values)[j], (*values)[i]
}

func sumThrough(values *[]int) int {
	total := 0
	for i := 0; i < len(*values); i++ {
		total += (*values)[i]
	}
	return total
}

func boundsProbe(values *[]int) (out string) {
	defer func() {
		if recover() != nil {
			out = "panicked"
		}
	}()
	_ = (*values)[len(*values)]
	return "unreachable"
}

func nilProbe() (out string) {
	var p *[]int
	defer func() {
		if recover() != nil {
			out = "nil-panicked"
		}
	}()
	_ = (*p)[0]
	return "unreachable"
}

func run() string {
	backing := make([]int, 0, 4)
	alias1 := &backing
	alias2 := &backing
	pushTwice(alias1, 10, 20)
	(*alias2)[0] = (*alias1)[0] + 100
	swap(alias1, 0, 1)
	grownSum := 0
	for round := 0; round < 6; round++ {
		pushTwice(alias2, round, round*2)
		grownSum += (*alias1)[len(*alias1)-1]
	}
	total := sumThrough(alias2)
	heap := []int{5, 3, 8, 1}
	swap(&heap, 0, 3)
	swap(&heap, 1, 2)
	return fmt.Sprintf("%d %d %v %d %s %s", total, grownSum, heap, len(*alias1), boundsProbe(alias1), nilProbe())
}
