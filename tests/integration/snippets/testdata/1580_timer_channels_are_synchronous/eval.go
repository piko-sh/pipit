package main

import (
	"fmt"
	"time"
)

func run() string {
	pending := time.NewTimer(time.Hour)
	defer pending.Stop()

	fired := time.NewTimer(time.Millisecond)
	<-fired.C
	fired.Reset(time.Millisecond)
	<-fired.C

	stopped := time.NewTimer(time.Hour)
	stoppedBeforeFiring := stopped.Stop()

	return fmt.Sprintf("pending=%d/%d fired=%d/%d stopped=%v,%d/%d",
		len(pending.C), cap(pending.C),
		len(fired.C), cap(fired.C),
		stoppedBeforeFiring, len(stopped.C), cap(stopped.C))
}
