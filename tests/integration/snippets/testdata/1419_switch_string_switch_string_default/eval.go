package main

func run() string {
	s := "z"
	switch s {
	case "a":
		s = "hello"
	default:
		s = "default"
	}
	return s
}
