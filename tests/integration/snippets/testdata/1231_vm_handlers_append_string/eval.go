package main

func run() string {
	s := []string{"a"}
	s = append(s, "b", "c")
	return s[2]
}
