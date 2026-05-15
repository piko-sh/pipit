package main

func run() string {
	s := "a"
	switch s {
	case "a":
		s = "hello"
	case "b":
		s = "world"
	}
	return s
}
