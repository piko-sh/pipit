package main

import "fmt"

func sum16(values []int16) int {
	total := 0
	for _, v := range values {
		total += int(v)
	}
	return total
}

func scale32(values []int32, factor int32) []int32 {
	for i := range values {
		values[i] *= factor
	}
	return values
}

func fold32(values []uint32) uint32 {
	var acc uint32 = 2166136261
	for _, v := range values {
		acc = (acc ^ v) * 16777619
	}
	return acc
}

func widen16(values []uint16) []uint32 {
	out := make([]uint32, 0, len(values))
	for _, v := range values {
		out = append(out, uint32(v)<<4)
	}
	return out
}

func overflowProbe(values []int16) int16 {
	values[0] = 32767
	values[0]++
	return values[0]
}

func run() string {
	a := []int16{1, -2, 300, -32768, 32767}
	b := []int32{10, -20, 1 << 30}
	c := []uint32{7, 4294967295, 12345}
	d := []uint16{1, 65535, 256}

	s := sum16(a)
	scaled := scale32(b, 3)
	folded := fold32(c)
	widened := widen16(d)
	over := overflowProbe(a)
	roundTrip := sum16(a)

	return fmt.Sprintf("%d %v %d %v %d %d", s, scaled, folded, widened, over, roundTrip)
}
