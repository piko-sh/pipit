package main

type token struct {
	kind  int
	value string
}

func EntrypointRun(n int) int {
	tokens := []token{}
	for i := 0; i < n; i++ {
		tokens = append(tokens, token{kind: i, value: "v"})
	}
	return len(tokens)
}
