package main

import "fmt"

func run() string {
	words := map[int][]string{
		7: {"keep", "me", "alive"},
	}
	labels := map[int]string{
		7: "label-seven",
	}

	heldSlice := words[7]
	heldLabel := labels[7]

	for i := 0; i < 10000; i++ {
		words[100+i] = []string{"filler"}
		labels[100+i] = "filler"
	}
	for i := 0; i < 5000; i++ {
		delete(words, 100+i)
		delete(labels, 100+i)
	}

	return fmt.Sprintf("%s-%s-%s %s %d %d",
		heldSlice[0], heldSlice[1], heldSlice[2], heldLabel, len(words), len(labels))
}
