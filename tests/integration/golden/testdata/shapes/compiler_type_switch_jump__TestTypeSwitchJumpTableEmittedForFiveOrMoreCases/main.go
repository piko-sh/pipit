package main

type point struct{ x, y int }
type box struct{ label string }

func classify(v any) string {
	switch t := v.(type) {
	case int:
		return "int:" + itoa(t)
	case string:
		return "string:" + t
	case float64:
		return "float"
	case point:
		return "point:" + itoa(t.x+t.y)
	case *box:
		return "boxptr:" + t.label
	case []int:
		return "ints:" + itoa(len(t))
	default:
		return "default"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

func EntrypointRun() string {
	var nothing any
	values := []any{7, "s", 2.5, point{x: 3, y: 4}, &box{label: "b"}, []int{1, 2, 3}, true, nothing, box{label: "value"}}
	out := ""
	for _, v := range values {
		out += classify(v) + ";"
	}
	return out
}
