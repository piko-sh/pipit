package main

func label(index int) string {
	digits := "0123456789"
	return "item-" + string(digits[index]) + "-tag"
}

func run() string {
	channel := make(chan string, 5)
	go func() {
		for i := 0; i < 5; i++ {
			channel <- label(i)
		}
		close(channel)
	}()
	out := ""
	for value := range channel {
		out += value + " "
	}
	return out
}
