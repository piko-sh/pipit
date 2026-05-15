package main

type Point struct {
	X int
	Y int
}

func origin() Point { return Point{X: 0, Y: 0} }
func EntrypointRun() int {
	point := origin()
	return point.X + point.Y
}
