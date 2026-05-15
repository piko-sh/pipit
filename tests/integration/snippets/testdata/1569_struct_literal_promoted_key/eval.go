package main

type Object struct {
	name  string
	color string
}

type Point3D struct {
	Object
	x, y, z float64
}

type Line struct {
	Object
	p, q Point3D
}

func run() string {
	line := Line{name: "diagonal", q: Point3D{y: -4, z: 12.3}}
	if line.Object.name != line.name {
		return "promoted key did not reach the embedded field"
	}
	return line.name + "/" + line.color + "/" + string(rune(int(line.q.z)+'0'))
}
