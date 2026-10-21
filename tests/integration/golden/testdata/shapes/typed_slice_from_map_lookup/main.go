package main

func EntrypointRun() int {
	transitions := map[string][]string{"a": {"b", "c", "d"}, "b": {"a"}}
	total := 0
	current := "a"
	for step := 0; step < 6; step++ {
		next, present := transitions[current]
		if !present || len(next) == 0 {
			break
		}
		for i := 0; i < len(next); i++ {
			total += len(next[i])
		}
		current = next[step%len(next)]
	}
	return total
}
