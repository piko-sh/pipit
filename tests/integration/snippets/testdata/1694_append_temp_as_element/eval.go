package main

import "fmt"

func boxed(s []any, x any) string {
	var all [][]any
	t := append(s, x)
	s = t
	grown := append(all, t)
	return fmt.Sprint(len(s), len(grown), len(grown[0]))
}

func typed(s []int, x int) string {
	var all [][]int
	t := append(s, x)
	s = t
	grown := append(all, t)
	return fmt.Sprint(len(s), len(grown), len(grown[0]))
}

func inPlace(s []any, x any) string {
	var all [][]any
	t := append(s, x)
	s = t
	all = append(all, t)
	return fmt.Sprint(len(s), len(all), len(all[0]))
}

func loop() string {
	var s []string
	var all [][]string
	for i := 0; i < 3; i++ {
		t := append(s, "x")
		s = t
		all = append(all[:len(all):len(all)], t)
	}
	out := ""
	for _, a := range all {
		out += fmt.Sprint(len(a))
	}
	return out + fmt.Sprint(len(s))
}

func run() string {
	return boxed([]any{1}, 2) + ";" + typed([]int{1}, 2) + ";" + inPlace([]any{1}, 2) + ";" + loop()
}
