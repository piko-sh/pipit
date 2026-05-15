package main

// EntrypointRun looks a []string up in a map and indexes it in a loop: the lookup is
// adopted into the string-slice bank, so the element reads and the length test use the
// direct typed-slice ops instead of reflect through the general bank.
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
