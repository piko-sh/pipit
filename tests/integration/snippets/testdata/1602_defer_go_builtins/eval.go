package main

import "fmt"

func closer(ch chan int) (closed bool) {
	defer func() {
		_, ok := <-ch
		closed = !ok
	}()
	defer close(ch)
	return
}

func deleter(m map[string]int) int {
	defer delete(m, "gone")
	return len(m)
}

func copier(destination, source []int) []int {
	defer copy(destination, source)
	return destination
}

func recoverer() (recovered bool) {
	defer func() {
		if r := recover(); r != nil {
			recovered = true
		}
	}()
	defer recover()
	panic("boom")
}

func deferredPanic() (caught any) {
	defer func() { caught = recover() }()
	defer panic("late")
	return
}

func run() string {
	ch := make(chan int)
	m := map[string]int{"gone": 1, "kept": 2}
	before := deleter(m)
	destination := make([]int, 2)
	copier(destination, []int{7, 8})
	return fmt.Sprint(closer(ch), " ", before, " ", len(m), " ", destination, " ", recoverer(), " ", deferredPanic())
}
