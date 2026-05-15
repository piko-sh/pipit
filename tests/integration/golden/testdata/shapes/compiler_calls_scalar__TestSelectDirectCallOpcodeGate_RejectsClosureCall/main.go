package main

func makeAdd(delta int) func(int) int {
	return func(value int) int { return value + delta }
}
func EntrypointRun() int {
	addOne := makeAdd(1)
	return addOne(41)
}
