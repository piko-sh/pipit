package main

import "fmt"

func run() string {
	m := map[string]int{"a": 1}
	_, okMap := m["a"]
	_, okMissing := m["zzz"]
	var x any = 3
	_, okAssert := x.(int)
	_, okWrong := x.(string)
	c := make(chan int, 1)
	c <- 1
	_, okRecv := <-c
	close(c)
	_, okClosed := <-c
	boxed := []any{okMap, okMissing, okAssert, okWrong, okRecv, okClosed, 1 < 2, len(m) == 0}
	sel := ""
	select {
	case v, ok := <-c:
		sel = fmt.Sprintf("%v %T %v", v, ok, ok)
	default:
		sel = "default"
	}
	var flag any = okMap
	_, flagIsBool := flag.(bool)
	return fmt.Sprintf("%T %v|%v|%s|%v", boxed[0], boxed, okMap && okAssert, sel, flagIsBool)
}
