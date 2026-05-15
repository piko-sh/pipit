package main

func run() int {
	x := 3
	y := 0
	switch x {
	case 1, 2, 3:
		y = 10
	case 4, 5:
		y = 20
	}
	return y
}
