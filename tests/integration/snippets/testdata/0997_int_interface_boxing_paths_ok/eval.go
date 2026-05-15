package main

import "fmt"

type holder struct {
	v interface{}
}

func id(x interface{}) interface{} { return x }
func get() interface{}             { return 7 }

func run() string {
	lit := []interface{}{7}
	var va interface{} = 7
	index := make([]interface{}, 1)
	index[0] = 7
	ml := map[string]interface{}{"k": 7}
	ma := map[string]interface{}{}
	ma["k"] = 7
	h := holder{v: 7}

	return fmt.Sprintf("%T %T %T %T %T %T %T %T",
		lit[0], va, index[0], ml["k"], ma["k"], id(7), get(), h.v)
}
