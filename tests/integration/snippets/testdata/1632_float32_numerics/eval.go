package main

import (
	"fmt"
	"math"
	"strings"
)

type Celsius float32

func scale(c Celsius, factor float32) Celsius {
	return c * Celsius(factor)
}

func run() string {
	var lines []string
	add := func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }

	var acc float32
	for range 10 {
		acc += 0.1
	}
	add("acc=%v eq1=%v bits=%d", acc, acc == 1, math.Float32bits(acc))

	var a, b float32 = 1.1, 2.2
	product := a * b
	add("product=%v %T bits=%d", product, any(product), math.Float32bits(product))
	add("sum=%v quotient=%v diff=%v", a+b, b/a, b-a)

	a *= 1.1
	a++
	add("compound=%v bits=%d", a, math.Float32bits(a))

	var tiny float32 = 1e-46
	add("tiny=%v zero=%v", tiny, tiny == 0)

	wide := float64(product)
	narrow := float32(wide * 3.3)
	add("wide=%.17g narrow=%v", wide, narrow)
	add("sqrt=%v", float32(math.Sqrt(float64(b))))

	c := Celsius(36.6)
	add("named=%v %T bits=%d", scale(c, 1.8), any(scale(c, 1.8)), math.Float32bits(float32(scale(c, 1.8))))

	var c64 complex64 = complex(0.1, 0.2)
	p := c64 * c64
	add("complex=%v %T", p, any(p))
	add("parts=%d %d", math.Float32bits(real(p)), math.Float32bits(imag(p)))

	var shift uint = 3
	add("shift=%v %v", shift<<1.0, 1<<shift)

	return strings.Join(lines, "\n")
}
