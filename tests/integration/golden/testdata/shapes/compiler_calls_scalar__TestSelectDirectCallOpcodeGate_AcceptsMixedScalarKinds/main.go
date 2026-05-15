package main

func mix(intValue int, uintValue uint, floatValue float64, boolValue bool, textValue string) int {
	if boolValue {
		return intValue + int(uintValue) + int(floatValue) + len(textValue)
	}
	return 0
}
func EntrypointRun() int {
	value := mix(1, 2, 3.0, true, "abcd")
	return value + 1
}
