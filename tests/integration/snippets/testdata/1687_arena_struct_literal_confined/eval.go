package main

import "strconv"

type token struct {
	kind  int
	value int
}

// evaluator is the expression-benchmark shape: a struct literal whose tokens field is
// stored after construction and whose address then feeds pointer-receiver methods that
// never publish the receiver.
type evaluator struct {
	tokens   []token
	position int
	depth    int
}

func tokenise(source string) []token {
	tokens := make([]token, 0, len(source))
	number := 0
	inNumber := false
	for i := 0; i < len(source); i++ {
		c := source[i]
		if c >= '0' && c <= '9' {
			number = number*10 + int(c-'0')
			inNumber = true
			continue
		}
		if inNumber {
			tokens = append(tokens, token{kind: 0, value: number})
			number, inNumber = 0, false
		}
		switch c {
		case '+':
			tokens = append(tokens, token{kind: 1})
		case '*':
			tokens = append(tokens, token{kind: 2})
		case '(':
			tokens = append(tokens, token{kind: 3})
		case ')':
			tokens = append(tokens, token{kind: 4})
		}
	}
	if inNumber {
		tokens = append(tokens, token{kind: 0, value: number})
	}
	return tokens
}

func (e *evaluator) peek() int {
	if e.position >= len(e.tokens) {
		return -1
	}
	return e.tokens[e.position].kind
}

func (e *evaluator) primary() int {
	t := e.tokens[e.position]
	e.position++
	if t.kind == 3 {
		e.depth++
		v := e.expression()
		e.position++
		e.depth--
		return v
	}
	return t.value
}

func (e *evaluator) term() int {
	v := e.primary()
	for e.peek() == 2 {
		e.position++
		v *= e.primary()
	}
	return v
}

func (e *evaluator) expression() int {
	v := e.term()
	for e.peek() == 1 {
		e.position++
		v += e.term()
	}
	return v
}

// storm allocates enough to recycle arena and heap storage several times over, so a
// tokens slice whose backing had been reclaimed would read garbage afterwards.
func storm(seed int) int {
	total := 0
	for i := 0; i < 400; i++ {
		block := make([]int, 64)
		block[i%64] = seed + i
		total += block[i%64]
		s := strconv.Itoa(total)
		total += len(s)
	}
	return total
}

func run() string {
	out := ""
	sources := []string{"1+2*3", "(1+2)*3", "12*(3+4)+5", "((7))", "2*2*2+1"}
	keep := make([]*evaluator, 0, len(sources))
	for round := 0; round < 3; round++ {
		for _, source := range sources {
			evaluator := evaluator{tokens: tokenise(source), position: 0}
			value := evaluator.expression()
			storm(value)
			// Re-read the tokens after the storm: the stored slice must still be intact.
			sum := 0
			for _, t := range evaluator.tokens {
				sum += t.kind*10 + t.value
			}
			out += strconv.Itoa(value) + ":" + strconv.Itoa(sum) + ":" + strconv.Itoa(evaluator.position) + ","
			if round == 2 {
				copyOf := evaluator
				keep = append(keep, &copyOf)
			}
		}
	}
	storm(99)
	for _, e := range keep {
		out += strconv.Itoa(len(e.tokens)) + strconv.Itoa(e.tokens[0].value)
	}
	return out
}
