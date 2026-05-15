package main

import (
	"bufio"
	"strings"
)

func run() string {
	r := bufio.NewReader(strings.NewReader("hello"))
	buffer := make([]byte, 5)
	n, _ := r.Read(buffer)
	return string(buffer[:n])
}
