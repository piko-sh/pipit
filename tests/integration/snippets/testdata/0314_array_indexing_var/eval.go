package main

func run() int {
	var arr [5]int
	for i := range arr {
		arr[i] = i * i
	}
	index := 3
	return arr[index] + arr[len(arr)-1]
}
