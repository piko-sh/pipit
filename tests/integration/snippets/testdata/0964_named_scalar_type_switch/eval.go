package main

import "fmt"

type Word string
type Count int

func classify(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case Word:
		return "Word"
	case int:
		return "int"
	case Count:
		return "Count"
	default:
		return "other"
	}
}

func classifyNamedFirst(v any) string {
	switch x := v.(type) {
	case Word:
		return "Word:" + string(x)
	case string:
		return "string:" + x
	default:
		return "other"
	}
}

func run() string {
	return fmt.Sprintf("%s %s %s %s | %s %s",
		classify("s"), classify(Word("w")), classify(3), classify(Count(3)),
		classifyNamedFirst("s"), classifyNamedFirst(Word("w")))
}
