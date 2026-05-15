package main

func classify(v any) int {
	switch v.(type) {
	case int:
		return 1
	case string:
		return 2
	case float64:
		return 3
	case bool:
		return 4
	}
	return 0
}
func EntrypointRun() int { return classify(1) + classify("s") }
