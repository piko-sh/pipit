package main

import "fmt"

type buffer struct {
	data []byte
}

type carrier struct {
	buffer
	tail []int
}

func growWhileScanning(b *buffer) (int, int) {
	steps := 0
	for i := 0; i < len(b.data); i++ {
		if len(b.data) < 10 {
			b.data = append(b.data, byte(len(b.data)))
		}
		steps++
		if steps > 50 {
			break
		}
	}
	return steps, len(b.data)
}

func shrinkWhileScanning(b *buffer) int {
	count := 0
	for i := 0; i < len(b.data); i++ {
		count++
		if i == 3 {
			b.data = b.data[:5]
		}
	}
	return count
}

func run() string {
	grower := &buffer{data: []byte{1, 2, 3}}
	steps, grown := growWhileScanning(grower)

	shrinker := &buffer{data: make([]byte, 8)}
	visited := shrinkWhileScanning(shrinker)

	swapped := &buffer{data: []byte("abc")}
	replacement := []byte("wxyz")
	before := len(swapped.data)
	swapped.data = replacement
	after := len(swapped.data)

	embedded := &carrier{buffer: buffer{data: []byte{9}}, tail: []int{1, 2, 3, 4}}
	promoted := len(embedded.data)
	direct := len(embedded.tail)

	return fmt.Sprintf("%d %d %d %d %d %d %d", steps, grown, visited, before, after, promoted, direct)
}
