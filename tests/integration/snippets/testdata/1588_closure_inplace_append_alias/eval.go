package main

func run() string {
	out := make([]byte, 0, 8)
	f := func() {
		saved := out
		out = append(out, 'x')
		out = append(out, 'y')
		saved = append(saved, 'z')
		_ = saved
	}
	f()
	return string(out)
}
