package main

import (
	"fmt"
	"strconv"
	"strings"
)

func perft(position *Position, depth int) int {
	if depth == 0 {
		return 1
	}
	total := 0
	mover := position.side
	for _, move := range position.pseudoMoves() {
		next := position.makeMove(move)
		if next.isAttacked(next.kings[mover], next.side) {
			continue
		}
		if depth == 1 {
			total++
			continue
		}
		total += perft(&next, depth-1)
	}
	return total
}

func entrypoint() string {
	position := startPosition()
	counts := make([]string, 0, 4)
	for depth := 1; depth <= 4; depth++ {
		counts = append(counts, strconv.Itoa(perft(&position, depth)))
	}
	return strings.Join(counts, " ")
}

func main() {
	fmt.Println(entrypoint())
}
