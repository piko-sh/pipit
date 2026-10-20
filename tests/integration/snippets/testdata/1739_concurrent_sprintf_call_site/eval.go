package main

import (
	"fmt"
	"strconv"
	"sync"
)

func label(worker, step int) string {
	return fmt.Sprintf("w%02d-s%03d", worker, step)
}

func pair(worker, step int) string {
	return fmt.Sprint(worker, ":", step)
}

func run() string {
	var mu sync.Mutex
	wrong := 0
	var wg sync.WaitGroup
	for worker := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bad := 0
			for step := range 400 {
				if label(worker, step) != "w"+fmt.Sprintf("%02d", worker)+"-s"+fmt.Sprintf("%03d", step) {
					bad++
				}
				if pair(worker, step) != strconv.Itoa(worker)+":"+strconv.Itoa(step) {
					bad++
				}
			}
			mu.Lock()
			wrong += bad
			mu.Unlock()
		}()
	}
	wg.Wait()
	return fmt.Sprint("wrong=", wrong)
}
