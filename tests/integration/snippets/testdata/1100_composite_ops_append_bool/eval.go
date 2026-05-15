package main

func run() bool {
	s := []bool{true}
	s = append(s, false)
	return s[1]
}
