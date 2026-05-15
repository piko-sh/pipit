package main

func run() string {
	result := ""
	for _, s := range []string{"a", "b", "c"} {
		result += s
	}
	return result
}
