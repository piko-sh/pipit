package main

func run() string {
	s := "b"
	switch s {
	case "a":
		s = "hello"
	case "b":
		s = "world"
	}
	return s
}
