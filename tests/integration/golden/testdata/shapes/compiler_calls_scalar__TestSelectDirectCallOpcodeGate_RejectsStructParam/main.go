package main

type Pair struct {
	Left  int
	Right int
}

func add(pair Pair) int  { return pair.Left + pair.Right }
func EntrypointRun() int { return add(Pair{Left: 3, Right: 4}) }
