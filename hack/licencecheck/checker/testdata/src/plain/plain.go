package plain

type OpResult int

func handleOutsideTheEngine(slots []any, value any, _, _ int) OpResult {
	slots[0] = value
	return 0
}
