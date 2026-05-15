package main

import "fmt"

type Word string

type Row []int

func run() string {
	byID := map[int64]Word{
		42: Word("answer"),
	}
	rows := map[int64]Row{
		42: {1, 2, 3},
	}

	w := byID[42]
	r := rows[42]

	var anyWord any = w
	_, isWord := anyWord.(Word)

	var anyRow any = r
	_, isRow := anyRow.(Row)

	return fmt.Sprintf("%T %T %v %v %s %d", w, r, isWord, isRow, w, r[2])
}
