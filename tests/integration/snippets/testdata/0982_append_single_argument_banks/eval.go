package main

import "fmt"

type point struct {
	x int
	y int
}

func run() string {
	ints := []int{1, 2, 3}
	keptInts := append(ints)
	bytes := []byte("hi")
	keptBytes := append(bytes)
	strings := []string{"a", "b"}
	keptStrings := append(strings)

	anys := []any{1, "two"}
	keptAnys := append(anys)
	points := []point{{x: 1, y: 2}}
	keptPoints := append(points)

	grow := make([]int, 0, 4)
	grow = append(grow, 7)
	grow = append(grow)

	chained := append(append(ints), 4, 5)

	return fmt.Sprintf("%d %d %d %d %d %d %d %d %d",
		len(keptInts), len(keptBytes), len(keptStrings), len(keptAnys),
		len(keptPoints), len(grow), grow[0], len(chained), chained[4])
}
