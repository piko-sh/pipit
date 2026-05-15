package main

func run() int {
	x := 0xFF
	x &^= 0x0F
	return x
}
