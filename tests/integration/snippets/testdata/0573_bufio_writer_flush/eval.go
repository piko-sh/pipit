package main

import (
	"bufio"
	"bytes"
)

func run() string {
	var buffer bytes.Buffer
	w := bufio.NewWriter(&buffer)
	_, _ = w.WriteString("hello")
	_ = w.Flush()
	return buffer.String()
}
