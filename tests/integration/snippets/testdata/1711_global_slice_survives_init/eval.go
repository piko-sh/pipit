package main

var names []string

func init() {
	names = []string{"TROO", "SHTG", "PUNG"}
}

func prepend(args []string) []string {
	return append([]string{"doom"}, args...)
}

func run() string {
	args := prepend([]string{"-warp", "1"})
	return names[0] + ":" + args[0]
}
