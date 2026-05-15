package main

import "strconv"

type reading struct {
	label string
	count int
	flags [4]byte
}

func makeReading(seed int) func() string {
	sample := reading{}
	sample.label = "r" + strconv.Itoa(seed)
	sample.count = seed * 3
	for index := range sample.flags {
		sample.flags[index] = byte('a' + seed + index)
	}
	captured := sample
	return func() string {
		return captured.label + ":" + strconv.Itoa(captured.count) + ":" + string(captured.flags[:])
	}
}

func makePointerReading(seed int) func() string {
	sample := reading{}
	sample.label = "p" + strconv.Itoa(seed)
	sample.count = seed
	pointer := &sample
	return func() string {
		return pointer.label + ":" + strconv.Itoa(pointer.count)
	}
}

func churnArena() int {
	scratch := 0
	for index := 0; index < 96; index++ {
		noise := reading{}
		noise.label = strconv.Itoa(index)
		noise.count = index
		for byteIndex := range noise.flags {
			noise.flags[byteIndex] = byte(index + byteIndex)
		}
		filler := noise.label + string(noise.flags[:])
		for byteIndex := 0; byteIndex < len(filler); byteIndex++ {
			scratch = scratch*7 + int(filler[byteIndex])
		}
	}
	return scratch
}

func run() string {
	first := makeReading(3)
	second := makePointerReading(11)
	churn := churnArena()
	return first() + "|" + second() + "|" + strconv.Itoa(churn)
}
