package main

func run() int64 {
	s := []any{int64(1)}
	return s[0].(int64)
}
