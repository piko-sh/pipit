package main

func run() int {
	var x any = 3.14
	result := 0
	switch x.(type) {
	case int:
		result = 1
	default:
		result = 3
	}
	return result
}
