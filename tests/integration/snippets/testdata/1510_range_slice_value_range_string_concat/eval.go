package main

func run() string {
	result := ""
	for _, s := range []string{"a", "b"} {
		result += s
	}
	return result
}
