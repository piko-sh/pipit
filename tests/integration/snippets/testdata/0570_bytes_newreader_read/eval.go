package main

import "bytes"

func run() string {
	r := bytes.NewReader([]byte("hello"))
	buffer := make([]byte, 5)
	n, _ := r.Read(buffer)
	return string(buffer[:n])
}
