package main

import "strconv"

// classify is a chain of byte comparisons against immediates, the shape a byte switch
// compiles to, with arms that fall through to the loop head, arms that jump forward past
// the chain, and a default that runs when no arm matches.
func classify(source string) (int, int, int, int) {
	letters, digits, spaces, others := 0, 0, 0, 0
	for i := 0; i < len(source); i++ {
		c := source[i]
		switch c {
		case 'a', 'e', 'i', 'o', 'u':
			letters += 2
		case ' ':
			spaces++
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			digits += int(c - '0')
		case '\n':
			spaces += 10
		default:
			if c >= 'a' && c <= 'z' {
				letters++
			} else {
				others++
			}
		}
	}
	return letters, digits, spaces, others
}

// interpret mirrors a tiny bytecode interpreter: a switch on the current byte drives a
// counter and a jump table, so taken and not-taken arms alternate on every step.
func interpret(program string) int {
	pointer, value, steps := 0, 0, 0
	for pc := 0; pc < len(program) && steps < 10000; pc++ {
		steps++
		switch program[pc] {
		case '>':
			pointer++
		case '<':
			pointer--
		case '+':
			value += pointer
		case '-':
			value -= pointer
		case '[':
			if value > 50 {
				pc += 2
			}
		case ']':
			if value < 0 {
				pc -= 4
			}
		}
	}
	return value*1000 + pointer*10 + steps
}

func run() string {
	l, d, s, o := classify("hello world 42\nabc xyz 7 !!")
	return strconv.Itoa(l) + "," + strconv.Itoa(d) + "," + strconv.Itoa(s) + "," + strconv.Itoa(o) + "|" +
		strconv.Itoa(interpret(">>++[-]<+>>++[--]<<<->>]-+")) + "|" + strconv.Itoa(interpret("+++++++++++[>>>>]"))
}
