package main

import "fmt"

type at40 [5]int64
type at64 [8]int64
type at128 [16]int64
type at136 [17]int64

func run() string {
	m40 := map[string]at40{"k": {1, 2, 3, 4, 5}}
	m64 := map[string]at64{"k": {1, 2, 3, 4, 5, 6, 7, 8}}
	m128 := map[string]at128{"k": {15: 9}}
	m136 := map[string]at136{"k": {16: 9}}
	m40["k"] = at40{}
	m64["k"] = at64{}
	m128["k"] = at128{}
	var z at136
	m136["k"] = z
	return fmt.Sprint(m40["k"] == at40{}, m64["k"] == at64{}, m128["k"] == at128{}, m136["k"] == at136{})
}
