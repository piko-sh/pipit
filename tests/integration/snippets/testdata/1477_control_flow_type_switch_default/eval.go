package main

func run() string {
	var x any = 3.14
	result := ""
	switch x.(type) {
	case int:
		result = "int"
	case string:
		result = "string"
	default:
		result = "other"
	}
	return result
}
