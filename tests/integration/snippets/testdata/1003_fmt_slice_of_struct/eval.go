package main

import "fmt"

type point struct {
	x int
	y int
}

type holder struct {
	points []point
	name   string
}

func run() string {
	pointSlice := []point{{x: 1, y: 2}, {x: 3, y: 4}}
	pointArray := [2]point{{x: 5, y: 6}, {x: 7, y: 8}}
	pointMap := map[string]point{"beta": {x: 3, y: 4}, "alpha": {x: 1, y: 2}}
	intMap := map[int]point{2: {x: 2, y: 2}, 10: {x: 10, y: 10}, 1: {x: 1, y: 1}}
	held := holder{points: []point{{x: 9, y: 9}}, name: "z"}
	nested := [][]point{{{x: 1, y: 1}}, {{x: 2, y: 2}, {x: 3, y: 3}}}

	plainInts := []int{1, 2, 3}
	plainMap := map[string]int{"b": 2, "a": 1}

	return fmt.Sprintf("slice=%v verbose=%+v array=%v smap=%v imap=%v held=%v held+=%+v nested=%v ints=%v imap2=%v",
		pointSlice, pointSlice, pointArray, pointMap, intMap, held, held, nested, plainInts, plainMap)
}
