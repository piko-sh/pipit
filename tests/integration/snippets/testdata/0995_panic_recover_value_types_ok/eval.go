package main

import "fmt"

type myErr struct {
	code int
}

func run() string {
	strForm := func() (r string) {
		defer func() {
			if v := recover(); v != nil {
				r = "str:" + v.(string)
			}
		}()
		panic("boom")
	}()

	structForm := func() (r string) {
		defer func() {
			if v := recover(); v != nil {
				r = fmt.Sprintf("struct:%d", v.(myErr).code)
			}
		}()
		panic(myErr{code: 7})
	}()

	nested := func() (r string) {
		defer func() {
			if v := recover(); v != nil {
				r = fmt.Sprintf("nested:%v", v)
			}
		}()
		func() { panic("inner") }()
		return "unreached"
	}()

	return strForm + "|" + structForm + "|" + nested
}
