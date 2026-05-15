package main

import (
	"fmt"
	"math/rand/v2"
)

type Tag int32

func run() string {
	r := rand.New(rand.NewPCG(42, 99))
	return fmt.Sprintf("%v %v %v %v %v | %v %v %v %v %v | %v | %v/%T %T %T",
		r.N(1000), r.N(int8(100)), r.N(int16(1000)), r.N(int32(100000)), r.N(int64(1<<40)),
		r.N(uint(1000)), r.N(uint8(100)), r.N(uint16(1000)), r.N(uint32(100000)), r.N(uint64(1<<40)),
		r.N(uintptr(500)),
		r.N(Tag(50)), r.N(Tag(50)), r.N(7), r.N[int16](33))
}
