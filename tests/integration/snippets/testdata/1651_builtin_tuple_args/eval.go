package main

import (
	"fmt"
	"strings"
)

func pair(a []byte) ([]byte, []byte) { return a, []byte("xyz") }

func mixed(a []byte) ([]byte, string) { return a, "hello" }

func entry(m map[int]int) (map[int]int, int) { return m, 1 }

func parts() (float64, float64) { return 1.5, -2 }

func run() string {
	var lines []string
	a := []byte{1, 2, 3, 4}
	lines = append(lines, fmt.Sprint(copy(pair(a)), a))
	b := []byte{1, 2}
	lines = append(lines, fmt.Sprint(copy(mixed(b)), b))
	m := map[int]int{1: 10, 2: 20}
	delete(entry(m))
	lines = append(lines, fmt.Sprint(len(m), m))
	c := complex(parts())
	lines = append(lines, fmt.Sprintf("%v %T", c, c))
	return strings.Join(lines, "\n")
}
