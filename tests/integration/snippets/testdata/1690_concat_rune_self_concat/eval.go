package main

func doubled(c rune) string {
	x := string(c)
	return x + x
}

func run() string {
	out := ""
	for _, c := range "xyz" {
		out += doubled(c) + ","
	}
	return out
}
