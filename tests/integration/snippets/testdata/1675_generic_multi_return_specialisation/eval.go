package main

type number interface {
	int | int64 | float64
}

func pick[T number](value any) (T, bool) {
	switch typed := value.(type) {
	case T:
		return typed, true
	case bool:
		if typed {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

func classify[T number](value any) string {
	switch value.(type) {
	case T:
		return "T"
	case string:
		return "string"
	}
	return "other"
}

func convert[T number](value any) (T, string) {
	picked, ok := pick[T](value)
	if ok {
		return picked, "picked"
	}
	return 0, "missed"
}

func run() string {
	out := ""
	if n, ok := pick[int]("42"); !ok {
		out += "s0:" + itoa(n) + ";"
	}
	if n, ok := pick[int](7); ok {
		out += "i7:" + itoa(n) + ";"
	}
	if f, ok := pick[float64](2.5); ok && f == 2.5 {
		out += "f;"
	}
	if n, ok := pick[int](true); ok {
		out += "b:" + itoa(n) + ";"
	}
	var wide int64
	var how string
	wide, how = convert[int64](int64(9))
	out += "c:" + itoa(int(wide)) + how + ";"
	wide, how = convert[int64]("nine")
	out += "c:" + itoa(int(wide)) + how + ";"
	out += classify[int]("42") + classify[int](7) + classify[float64](7)
	return out
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}
