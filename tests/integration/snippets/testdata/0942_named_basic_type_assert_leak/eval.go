package main

import "fmt"

type Word string

func run() string {
	var anyWord any = Word("answer")

	_, isPlain := anyWord.(string)
	_, isWord := anyWord.(Word)

	return fmt.Sprintf("%v %v", isPlain, isWord)
}
