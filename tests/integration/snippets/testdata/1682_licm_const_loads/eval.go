package main

import "strconv"

const program = "+>+<-[]>.<,+-][><"

func classifyProgram(steps int) int {
	score := 0
	for pc := 0; pc < steps && pc < len(program); pc++ {
		switch program[pc] {
		case '+':
			score += 3
		case '-':
			score -= 1
		case '>', '<':
			score += 10
		case '[', ']':
			score *= 2
		default:
			score++
		}
	}
	return score
}

func zeroTripInt(n int) int {
	v := 1
	for i := 0; i < n; i++ {
		v = 42
	}
	return v
}

func zeroTripUint(n int) uint {
	var v uint = 7
	for i := 0; i < n; i++ {
		v = 1000000
	}
	return v
}

func zeroTripFloat(n int) float64 {
	v := 0.5
	for i := 0; i < n; i++ {
		v = 2.25
	}
	return v
}

func zeroTripString(n int) string {
	v := "before"
	for i := 0; i < n; i++ {
		v = "inside"
	}
	return v
}

func breakBeforeUse(limit int) string {
	out := ""
	for i := 0; i < 10; i++ {
		if i == limit {
			break
		}
		out += program[i : i+1]
		if i == limit+2 {
			break
		}
	}
	return out
}

func nested(n int) int {
	total := 0
	for outer := 0; outer < n; outer++ {
		for inner := 0; inner < outer && inner < len(program); inner++ {
			if program[inner] == '+' {
				total += outer
			}
		}
	}
	return total
}

func resultWrittenInLoop(n int) string {
	s := "start"
	for i := 0; i < n; i++ {
		s = "loop"
		if i == 2 {
			return s + strconv.Itoa(i)
		}
	}
	return s
}

func run() string {
	out := ""
	for _, n := range []int{0, 1, 5, 17} {
		out += strconv.Itoa(n) + ":" + strconv.Itoa(classifyProgram(n)) + "," +
			strconv.Itoa(zeroTripInt(n)) + "," + strconv.FormatUint(uint64(zeroTripUint(n)), 10) + "," +
			strconv.FormatFloat(zeroTripFloat(n), 'f', 2, 64) + "," + zeroTripString(n) + "," +
			breakBeforeUse(n) + "," + strconv.Itoa(nested(n)) + "," + resultWrittenInLoop(n) + ";"
	}
	return out
}
