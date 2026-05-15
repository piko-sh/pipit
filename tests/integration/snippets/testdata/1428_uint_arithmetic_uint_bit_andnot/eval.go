package main

func run() uint {
	var a uint = 0xFF
	var b uint = 0x0F
	return a &^ b
}
