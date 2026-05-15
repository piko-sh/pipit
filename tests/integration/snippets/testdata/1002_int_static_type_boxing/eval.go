package main

import "fmt"

func recoverType() (out string) {
	defer func() {
		switch value := recover().(type) {
		case int:
			out = fmt.Sprintf("recovered int %d", value)
		case int64:
			out = fmt.Sprintf("recovered int64 %d", value)
		default:
			out = fmt.Sprintf("recovered %T", value)
		}
	}()
	panic(42)
}

func run() string {
	recovered := recoverType()

	anys := []any{}
	anys = append(anys, 7)
	anys = append(anys, int32(9))
	anys = append(anys, "text")
	_, appendedIntIsInt := anys[0].(int)
	appendedTypes := fmt.Sprintf("%T %T %T", anys[0], anys[1], anys[2])

	channel := make(chan any, 2)
	channel <- 5
	channel <- uint(3)
	first := <-channel
	second := <-channel
	_, sentIntIsInt := first.(int)
	channelTypes := fmt.Sprintf("%T %T", first, second)

	return fmt.Sprintf("%s | append[%v] %s | chan[%v] %s",
		recovered, appendedIntIsInt, appendedTypes, sentIntIsInt, channelTypes)
}
