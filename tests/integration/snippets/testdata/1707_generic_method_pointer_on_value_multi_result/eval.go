package main

type Stack[T any] struct {
	items []T
}

func (s *Stack[T]) Push(item T) { s.items = append(s.items, item) }

func (s *Stack[T]) Pop() (T, bool) {
	var zero T
	if len(s.items) == 0 {
		return zero, false
	}
	item := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return item, true
}

func run() int {
	var s Stack[int]
	for i := 1; i <= 4; i++ {
		s.Push(i)
	}
	total := 0
	for {
		item, ok := s.Pop()
		if !ok {
			break
		}
		total = total*10 + item
	}
	return total
}
