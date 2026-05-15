package main

func EntrypointRun() int {
	total := 0
	for i := 0; i < 32; i++ {
		total += i * 3
	}
	return total
}
