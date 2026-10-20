package main

import "fmt"

type at128 [16]int64
type at129 [129]byte
type at136 [17]int64
type at256 [32]int64
type at4096 [512]int64

func checksum128() int64 {
	m := map[string]at128{}
	for i := range 20 {
		var v at128
		v[0], v[15] = int64(i), int64(i*7)
		m[fmt.Sprint("k", i)] = v
	}
	var s int64
	for i := range 20 {
		v := m[fmt.Sprint("k", i)]
		s += v[0] + v[15]
	}
	return s
}

func checksum129() int64 {
	m := map[string]at129{}
	for i := range 20 {
		var v at129
		v[0], v[128] = byte(i), byte(i*3)
		m[fmt.Sprint("k", i)] = v
	}
	var s int64
	for i := range 20 {
		v := m[fmt.Sprint("k", i)]
		s += int64(v[0]) + int64(v[128])
	}
	return s
}

func checksum136() int64 {
	m := map[string]at136{}
	for i := range 20 {
		var v at136
		v[0], v[16] = int64(i), int64(i*11)
		m[fmt.Sprint("k", i)] = v
	}
	var s int64
	for i := range 20 {
		v := m[fmt.Sprint("k", i)]
		s += v[0] + v[16]
	}
	return s
}

func checksum256() int64 {
	m := map[string]at256{}
	for i := range 20 {
		var v at256
		v[0], v[31] = int64(i), int64(i*13)
		m[fmt.Sprint("k", i)] = v
	}
	var s int64
	for i := range 20 {
		v := m[fmt.Sprint("k", i)]
		s += v[0] + v[31]
	}
	return s
}

func checksum4096() int64 {
	m := map[string]at4096{}
	for i := range 20 {
		var v at4096
		v[0], v[511] = int64(i), int64(i*17)
		m[fmt.Sprint("k", i)] = v
	}
	var s int64
	for i := range 20 {
		v := m[fmt.Sprint("k", i)]
		s += v[0] + v[511]
	}
	return s
}

func run() string {
	return fmt.Sprint(checksum128(), checksum129(), checksum136(), checksum256(), checksum4096())
}
