package main

import "fmt"

type Point3D struct{ x, y, z float64 }

type Object struct{ name, color string }

type Line struct {
	Object
	p, q Point3D
}

func run() string {
	object := Object{name: "line", color: "black"}

	line1 := Line{Object: object, q: Point3D{x: 1, y: 1, z: 1}}
	line2 := Line{name: "diagonal", q: Point3D{x: 1, y: 1, z: 1}}
	line3 := Line{name: "diagonal", color: "red", p: Point3D{x: 2}}

	return fmt.Sprintf("%v|%v|%v|%v|%v",
		line1.name, line1.q.z, line2.name, line2.color, line3.color) +
		fmt.Sprintf("|%v|%v", line3.p.x, line3.Object.name)
}
