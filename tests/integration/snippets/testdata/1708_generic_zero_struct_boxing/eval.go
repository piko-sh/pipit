package main

import "encoding/binary"

type record struct {
	Offset int32
	Size   int32
	Name   [8]byte
}

func recordSize[T any]() int {
	var value T
	return binary.Size(value)
}

func run() int {
	return recordSize[record]()
}
