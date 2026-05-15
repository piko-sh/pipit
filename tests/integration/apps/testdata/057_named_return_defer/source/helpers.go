package main

func adjustResult(r *int, delta int) {
	*r += delta
}

func wrapErrorMessage(message string, source string) string {
	return source + ": " + message
}
