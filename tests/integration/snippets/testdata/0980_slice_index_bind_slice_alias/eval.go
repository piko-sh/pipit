package main

import "fmt"

func run() string {
	rows := make([][]int, 0, 4)
	rows = append(rows, []int{1, 2, 3})
	held := rows[len(rows)-1]

	rows = rows[:len(rows)-1]
	rows = append(rows, []int{9, 9})

	return fmt.Sprintf("%d %d %d %d", len(held), held[0], len(rows[0]), rows[0][0])
}
