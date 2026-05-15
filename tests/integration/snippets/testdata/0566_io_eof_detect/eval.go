package main

import (
	"io"
	"strings"
)

func run() int {
	r := strings.NewReader("hi")
	buffer := make([]byte, 10)
	_, _ = r.Read(buffer)
	_, err := r.Read(buffer)
	if err == io.EOF {
		return 1
	}
	return 0
}
