package main

import "fmt"

func readAt(values []int, index int) (result int, caught string) {
	defer func() {
		if r := recover(); r != nil {
			caught = fmt.Sprint(r)
		}
	}()
	result = values[index]
	return result, ""
}

func writeAt(values []int, index int, v int) (caught string) {
	defer func() {
		if r := recover(); r != nil {
			caught = fmt.Sprint(r)
		}
	}()
	values[index] = v
	return ""
}

func run() string {
	values := make([]int, 4)
	for i := 0; i < 4; i++ {
		values[i] = i * 10
	}

	okVal, okErr := readAt(values, 2)
	_, oobErr := readAt(values, 4)
	_, negErr := readAt(values, -1)
	wOob := writeAt(values, 99, 7)
	wOk := writeAt(values, 3, 77)

	total := 0
	for i := 0; i < len(values); i++ {
		total += values[i]
	}

	return fmt.Sprintf("%d|%s|%s|%s|%s|%s|%d", okVal, okErr, oobErr, negErr, wOob, wOk, total)
}
