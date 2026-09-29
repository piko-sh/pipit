package main

import "fmt"

func withDefer(f func(int), x, a int) int {
	y := x
	defer f(y)
	y = a + 1
	return y
}

func deferThenRead(f func(int), x int) int {
	y := x
	defer f(y)
	return y * 3
}

func goThenRead(f func(int), x int) int {
	y := x
	go f(y)
	return y * 5
}

func withDeferString(f func(string), x, a string) string {
	y := x
	defer f(y)
	y = a + "!"
	return y
}

func deferStringThenRead(f func(string), x string) string {
	y := x
	defer f(y)
	return y + "?"
}

func run() string {
	deferred := 0
	returned := withDefer(func(v int) { deferred = v }, 7, 100)

	deferredRead := 0
	returnedRead := deferThenRead(func(v int) { deferredRead = v }, 9)

	done := make(chan int)
	spawnedReturn := goThenRead(func(v int) { done <- v }, 11)
	spawned := <-done

	deferredString := ""
	returnedString := withDeferString(func(v string) { deferredString = v }, "in", "out")

	deferredStringRead := ""
	returnedStringRead := deferStringThenRead(func(v string) { deferredStringRead = v }, "q")

	return fmt.Sprint(deferred, returned, deferredRead, returnedRead, spawned, spawnedReturn,
		deferredString, returnedString, deferredStringRead, returnedStringRead)
}
