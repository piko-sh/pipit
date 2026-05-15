package main

func run() int {
	x := 99
	switch x {
	case 1:
		x = 10
	default:
		x = -1
	}
	return x
}
