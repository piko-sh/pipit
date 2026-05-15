package main

func run() int {
	x := 3
	switch x {
	case 1:
		x = 10
	case 2:
		x = 20
	case 3:
		x = 30
	}
	return x
}
