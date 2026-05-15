package main

import (
	"fmt"
	"strings"
)

type Slice []int
type Int4 [4]int
type PInt4 *[4]int

func catch(f func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprint(r)
		}
	}()
	f()
	return "no panic"
}

func run() string {
	var lines []string
	s := make([]byte, 8, 10)
	for i := range s {
		s[i] = byte(i)
	}
	p := (*[8]byte)(s)
	p[0] = 42
	lines = append(lines, fmt.Sprint(s[0], *p, [8]byte(s) == *p, [4]byte(s)))
	arr := [8]byte(s)
	arr[1] = 99
	lines = append(lines, fmt.Sprint(s[1], arr[1]))
	lines = append(lines, catch(func() { _ = (*[9]byte)(s) }))
	lines = append(lines, catch(func() { _ = [9]byte(s) }))
	var n []byte
	lines = append(lines, fmt.Sprint((*[0]byte)(n) == nil, [0]byte(n), (*[0]byte)(make([]byte, 0)) == nil))
	var np *[]byte
	lines = append(lines, catch(func() { _ = [0]byte(*np) }))
	ii := make(Slice, 4)
	q := (*Int4)(ii)
	q[2] = 7
	r := PInt4(ii)
	r[3] = 9
	lines = append(lines, fmt.Sprint(ii, *q, Int4(ii)))
	return strings.Join(lines, "\n")
}
