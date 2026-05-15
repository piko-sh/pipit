package main

func run() int {
	var x any = 42
	result := 0
	switch x.(type) {
	case int:
		result = 1
	case string:
		result = 2
	}
	return result
}
