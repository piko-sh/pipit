package main

func run() int {
	x := 2
	switch x {
	case 1, 2, 3:
		x = 10
	case 4, 5:
		x = 20
	}
	return x
}
