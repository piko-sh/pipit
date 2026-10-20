package main

func compute(seed int) int {
	base := seed + 1
	doubled := func() int { return base * 2 }()
	tail := base + 100
	return doubled*1000 + tail
}

func run() int {
	return compute(3) + compute(7)
}
