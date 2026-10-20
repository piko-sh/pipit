// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

//go:build unix

package app

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
)

func TestSIGTERMReachesAHostWhileACallbackSpins(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	require.NoError(t, err)
	for _, dispatch := range []string{"asm", "go"} {
		t.Run(dispatch+" dispatch on one P", func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), cancellationWatchdog)
			defer cancel()
			child := "TestSIGTERMReachesAHostWhileACallbackSpinsChild"
			command := exec.CommandContext(ctx, executable, "-test.run=^"+child+"$", "-test.count=1", "-test.v")
			command.Env = append(os.Environ(), cancellationChildEnv+"="+child, cancellationDispatchEnv+"="+dispatch, "GOMAXPROCS=1")
			command.WaitDelay = time.Second
			stdout, err := command.StdoutPipe()
			require.NoError(t, err)
			require.NoError(t, command.Start())

			lines := bufio.NewScanner(stdout)
			var seen []string
			ready := false
			for lines.Scan() {
				seen = append(seen, lines.Text())
				if strings.Contains(lines.Text(), "callback spinning") {
					ready = true
					break
				}
			}
			require.True(t, ready, "the child never reported its spinning callback: %s", strings.Join(seen, "\n"))
			signalled := time.Now()
			require.NoError(t, command.Process.Signal(syscall.SIGTERM))
			for lines.Scan() {
				seen = append(seen, lines.Text())
			}
			require.NoError(t, command.Wait(), "%s", strings.Join(seen, "\n"))
			require.NoError(t, ctx.Err(), "the child ignored SIGTERM: %s", strings.Join(seen, "\n"))
			require.Less(t, time.Since(signalled), 5*time.Second)
			require.Contains(t, strings.Join(seen, "\n"), "host stopped on SIGTERM")
		})
	}
}

func TestSIGTERMReachesAHostWhileACallbackSpinsChild(t *testing.T) {
	cancellationChild(t, "TestSIGTERMReachesAHostWhileACallbackSpinsChild")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM)
	defer signal.Stop(stop)

	var kept func()
	harness, vm := newCancellationVM(t, t.Context(), map[string]reflect.Value{
		"Keep": reflect.ValueOf(func(fn func()) { kept = fn }),
	}, engine.VMLimits{CallbackTimeout: 15 * time.Second})
	_, err := vm.Execute(harness.compile(t, `import "host"
host.Keep(func() {
	n := 0
	for {
		n++
	}
})`))
	require.NoError(t, err)
	go kept()
	time.Sleep(20 * time.Millisecond)
	fmt.Println("callback spinning")
	<-stop
	fmt.Println("host stopped on SIGTERM")
	os.Exit(0)
}
