package main

// compute captures a read-only scalar in an immediately invoked function expression from
// a frame that is not the root, then keeps using the frame afterwards. The assembly
// dispatch loop once restored this frame's code base from a slot that had never been
// written, so the result came back as zero.
func compute(seed int) int {
	base := seed + 1
	doubled := func() int { return base * 2 }()
	tail := base + 100
	return doubled*1000 + tail
}

func run() int {
	return compute(3) + compute(7)
}
