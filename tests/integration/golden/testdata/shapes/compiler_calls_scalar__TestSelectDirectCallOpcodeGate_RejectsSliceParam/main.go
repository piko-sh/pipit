package main

func sumSlice(values []int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}
func EntrypointRun() int {
	values := []int{1, 2, 3, 4, 5}
	return sumSlice(values)
}
