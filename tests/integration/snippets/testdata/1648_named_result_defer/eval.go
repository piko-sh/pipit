package main

import (
	"errors"
	"fmt"
	"strings"
)

func twoDefers() (a, b int) {
	defer func() { a *= 2 }()
	defer func() { b += 10 }()
	return 21, 11
}

func recoverSets() (n int, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("recovered: %v", r)
			n = -1
		}
	}()
	defer func() { n++ }()
	return 5, nil
}

func recoverPanics() (n int, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("recovered: %v", r)
			n = -1
		}
	}()
	defer func() { n++ }()
	panic("boom")
}

func appendSuffix() (s string) {
	defer func() { s += "!" }()
	return "hi"
}

func loopDefers() (total int) {
	for i := 1; i <= 3; i++ {
		defer func() { total += i }()
	}
	return 100
}

func wrapErr() (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("wrapped: %w", err)
		}
	}()
	return errors.New("base")
}

func mixed() (x int, y string) {
	defer func() { y = fmt.Sprint(y, x) }()
	x, y = 7, "v"
	return x + 1, y + "w"
}

func run() string {
	var lines []string
	a, b := twoDefers()
	lines = append(lines, fmt.Sprint(a, b))
	n, err := recoverSets()
	lines = append(lines, fmt.Sprint(n, err))
	n, err = recoverPanics()
	lines = append(lines, fmt.Sprint(n, err))
	lines = append(lines, appendSuffix())
	lines = append(lines, fmt.Sprint(loopDefers()))
	lines = append(lines, fmt.Sprint(wrapErr()))
	x, y := mixed()
	lines = append(lines, fmt.Sprint(x, y))
	return strings.Join(lines, "\n")
}
