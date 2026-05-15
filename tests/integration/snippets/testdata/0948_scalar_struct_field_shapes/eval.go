package main

import "fmt"

type mixed struct {
	pad8  int8
	pad16 int16
	pad32 int32
	i     int
	i64   int64
	u     uint
	u64   uint64
	f64   float64
	f32   float32
	b     bool
	u8    uint8
	u32   uint32
}

type inner struct {
	depth int
	hot   float64
}

type outer struct {
	inner
	label bool
}

func pointerReceiver() string {
	m := &mixed{pad8: -1, pad16: -300, pad32: -70000}
	for i := 0; i < 50; i++ {
		m.i += i
		m.i64 += int64(i) * 3
		m.u += uint(i)
		m.u64 += uint64(i) * 7
		m.f64 += float64(i) * 0.5
		m.f32 += float32(i) * 0.25
		m.b = m.i%2 == 1
		m.u8 += uint8(i)
		m.u32 += uint32(i) * 11
	}
	return fmt.Sprintf("%d %d %d %d %.2f %.2f %v %d %d %d %d %d",
		m.i, m.i64, m.u, m.u64, m.f64, m.f32, m.b, m.u8, m.u32,
		m.pad8, m.pad16, m.pad32)
}

func sliceElementReceiver() string {
	bodies := make([]mixed, 4)
	for pass := 0; pass < 25; pass++ {
		for j := range bodies {
			bodies[j].i += j + pass
			bodies[j].f64 += float64(j) + 0.125
			bodies[j].u64 += uint64(pass)
			bodies[j].b = bodies[j].i > 30
		}
	}
	total := 0
	var fsum float64
	var usum uint64
	trues := 0
	for j := range bodies {
		total += bodies[j].i
		fsum += bodies[j].f64
		usum += bodies[j].u64
		if bodies[j].b {
			trues++
		}
	}
	return fmt.Sprintf("%d %.3f %d %d", total, fsum, usum, trues)
}

func embeddedFields() string {
	o := &outer{}
	for i := 0; i < 20; i++ {
		o.depth += i
		o.hot += float64(i) * 1.5
		o.label = o.depth%3 == 0
	}
	return fmt.Sprintf("%d %.1f %v", o.depth, o.hot, o.label)
}

func run() string {
	a := pointerReceiver()
	b := sliceElementReceiver()
	c := embeddedFields()
	return a + " | " + b + " | " + c
}
