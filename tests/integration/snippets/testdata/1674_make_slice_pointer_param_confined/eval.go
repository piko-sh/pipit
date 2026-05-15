package main

func generateExpression(state uint32) (string, uint32) {
	output := make([]byte, 0, 64)
	state = generateTerm(&output, state, 3)
	return string(output), state
}

func nextState(state uint32) uint32 {
	return state*1664525 + 1013904223
}

func generateTerm(output *[]byte, state uint32, depthBudget int) uint32 {
	state = nextState(state)
	if depthBudget == 0 || state%3 == 0 {
		value := int(state % 100)
		*output = append(*output, intToDecimalBytes(value)...)
		return state
	}
	*output = append(*output, '(')
	state = generateTerm(output, state, depthBudget-1)
	operators := []byte{'+', '-', '*'}
	*output = append(*output, operators[int(state%3)])
	state = generateTerm(output, state, depthBudget-1)
	*output = append(*output, ')')
	return state
}

func intToDecimalBytes(value int) []byte {
	if value == 0 {
		return []byte{'0'}
	}
	var digits [4]byte
	position := len(digits)
	for value > 0 {
		position--
		digits[position] = byte('0' + value%10)
		value /= 10
	}
	return digits[position:]
}

func run() string {
	state := uint32(42)
	total := 0
	var last string
	for i := 0; i < 200; i++ {
		var expression string
		expression, state = generateExpression(state)
		total += len(expression)
		last = expression
	}
	return last + " " + intToString(total)
}

func intToString(value int) string {
	return string(intToDecimalBytes(value))
}
