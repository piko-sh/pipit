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

package sandboxlinux

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

type serviceAdmissionContender struct {
	command     *exec.Cmd
	input       io.WriteCloser
	output      *bufio.Reader
	diagnostics bytes.Buffer
}

func TestNativeServiceAdmissionCompetingHosts(t *testing.T) {
	parent := os.Getenv("PIPIT_TEST_CGROUP_PARENT")
	if parent == "" {
		t.Skip("requires explicitly delegated cgroup-v2 parent")
	}
	path := filepath.Join(parent, serviceGroupNameFor(testTenant))
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("fixture requires an unoccupied service slot: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const contenders = 6
	hosts := make([]*serviceAdmissionContender, 0, contenders)
	for range contenders {
		hosts = append(hosts, startServiceAdmissionContender(t, ctx))
	}
	for _, host := range hosts {
		if _, err := host.input.Write([]byte{1}); err != nil {
			t.Fatal(err)
		}
	}
	var winner *serviceAdmissionContender
	for _, host := range hosts {
		status, err := host.output.ReadString('\n')
		if err != nil {
			t.Fatalf("missing admission result: %v", err)
		}
		switch status {
		case "held\n":
			if winner != nil {
				t.Fatal("multiple hosts acquired the same service reservation")
			}
			winner = host
		case "busy\n":
		default:
			t.Fatalf("unexpected admission result: %q", status)
		}
	}
	if winner == nil {
		t.Fatal("no host acquired the empty reservation")
	}
	if err := winner.command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := winner.command.Wait(); err == nil {
		t.Fatal("host survived forced termination")
	}
	status, ok := winner.command.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("host did not die from SIGKILL: %v", winner.command.ProcessState)
	}
	for _, host := range hosts {
		if host != winner {
			if err := host.command.Wait(); err != nil {
				t.Fatalf("rejected host failed: %v %s", err, host.diagnostics.String())
			}
		}
	}
	if owner, err := newServiceGroup(parent, Limits{}, testTenant); owner != nil || !errors.Is(err, ErrServiceBusy) {
		t.Fatalf("host death released unconfirmed reservation: %v", err)
	}
}

func TestNativeServiceAdmissionRaceChild(t *testing.T) {
	if os.Getenv("PIPIT_SERVICE_RACE_CHILD") != "1" {
		return
	}
	var signal [1]byte
	if _, err := io.ReadFull(os.Stdin, signal[:]); err != nil {
		t.Fatal(err)
	}
	group, err := newServiceGroup(os.Getenv("PIPIT_TEST_CGROUP_PARENT"), Limits{}, testTenant)
	if errors.Is(err, ErrServiceBusy) {
		fmt.Println("busy")
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("held")
	_, _ = io.ReadFull(os.Stdin, signal[:])
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := group.closeGroup(ctx); err != nil {
		t.Fatal(err)
	}
}

func startServiceAdmissionContender(t *testing.T, ctx context.Context) *serviceAdmissionContender {
	t.Helper()
	var host serviceAdmissionContender
	host.command = exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeServiceAdmissionRaceChild$")
	host.command.Env = append(os.Environ(), "PIPIT_SERVICE_RACE_CHILD=1")
	host.command.Stderr = &host.diagnostics
	input, err := host.command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	host.input = input
	output, err := host.command.StdoutPipe()
	if err != nil {
		input.Close()
		t.Fatal(err)
	}
	host.output = bufio.NewReader(output)
	if err := host.command.Start(); err != nil {
		input.Close()
		output.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if host.command.ProcessState == nil {
			_ = host.command.Process.Kill()
			_ = host.command.Wait()
		}
		_ = host.input.Close()
	})
	return &host
}
