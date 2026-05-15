package main

import "fmt"

func run() string {
	var asInt any = int(1)
	var asInt64 any = int64(1)
	var asInt32 any = int32(1)
	var alsoInt any = int(1)
	return fmt.Sprintf("intVsInt64=%v;intVsInt32=%v;intVsInt=%v",
		asInt == asInt64, asInt == asInt32, asInt == alsoInt)
}
