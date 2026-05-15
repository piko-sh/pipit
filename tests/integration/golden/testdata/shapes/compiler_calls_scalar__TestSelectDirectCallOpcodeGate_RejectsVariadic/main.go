package main

func sumAll(values ...int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}
func EntrypointRun() int { return sumAll(1, 2, 3, 4, 5) }
