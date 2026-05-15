//go:build linux && (amd64 || arm64)

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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestServiceRecoveryHostLiveAndDead(t *testing.T) {
	command := exec.Command("/bin/sleep", "30")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	}()
	descriptor, err := unix.PidfdOpen(command.Process.Pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	host := os.NewFile(uintptr(descriptor), "test-original-host")
	defer host.Close()
	identity, err := watchdogFileIdentity(host)
	if err != nil {
		t.Fatal(err)
	}
	original := serviceRecoveryIdentity{PID: command.Process.Pid, Host: identity}
	if err := verifyRecoveryHost(original); !errors.Is(err, ErrServiceBusy) {
		t.Fatal("live original host accepted:", err)
	}
	replaced := original
	replaced.Host[1] ^= 1
	if err := verifyRecoveryHost(replaced); !errors.Is(err, ErrUnavailable) {
		t.Fatal("different process identity accepted:", err)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := waitHostLifetime(host, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := verifyRecoveryHost(original); err != nil {
		t.Fatal("matching terminated host rejected:", err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("killed host exited successfully")
	}
	if err := verifyRecoveryHost(original); err != nil {
		t.Fatal("reaped original host rejected:", err)
	}
}

func TestServiceRecoveryIdentityDecoding(t *testing.T) {
	identity := serviceRecoveryIdentity{
		Profile: "linux-service-recovery-v1", Name: serviceGroupNameFor(testTenant), PID: 123, Host: [2]uint64{1, 2},
		Parent: cgroupDirectoryID{Major: 0, Minor: 1, Inode: 1, Mount: 1},
		Group:  cgroupDirectoryID{Major: 0, Minor: 1, Inode: 2, Mount: 1},
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := decodeServiceRecoveryIdentity(encoded); err != nil || decoded != identity {
		t.Fatal("valid identity rejected:", err)
	}
	for _, data := range [][]byte{
		nil, bytes.Repeat([]byte("x"), maximumServiceRecoveryIdentityBytes+1),
		append(bytes.Clone(encoded), ' '),
		bytes.Replace(encoded, []byte(`"PID":123`), []byte(`"PID":0`), 1),
		bytes.Replace(encoded, []byte(`"PID":123`), []byte(`"PID":2147483648`), 1),
		bytes.Replace(encoded, []byte(`"PID":123`), []byte(`"PID":123,"PID":124`), 1),
		bytes.Replace(encoded, []byte(`"PID":123`), []byte(`"PID":123,"extra":true`), 1),
		bytes.Replace(encoded, []byte("linux-service-recovery-v1"), []byte("unknown-profile"), 1),
		bytes.Replace(encoded, []byte(`"Mount":1`), []byte(`"Mount":0`), 1),
	} {
		if decoded, err := decodeServiceRecoveryIdentity(data); err == nil || decoded != (serviceRecoveryIdentity{}) {
			t.Fatal("invalid identity accepted:", string(data), err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifyServiceRecoveryHost(ctx, nil, [sha256.Size]byte{}, [sha256.Size]byte{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled verification continued:", err)
	}
	if err := verifyServiceRecoveryHost(nil, nil, [sha256.Size]byte{}, [sha256.Size]byte{}); !errors.Is(err, ErrInvalidLimits) {
		t.Fatal("missing context accepted:", err)
	}
	if err := verifyServiceRecoveryHost(context.Background(), encoded, sha256.Sum256(encoded), [sha256.Size]byte{1}); err == nil {
		t.Fatal("raw service identity accepted in place of approved checkpoint")
	}
}

func FuzzServiceRecoveryIdentity(fuzzer *testing.F) {
	fuzzer.Add([]byte("{}"))
	seed, err := json.Marshal(serviceRecoveryIdentity{
		Profile: "linux-service-recovery-v1", PID: 123, Host: [2]uint64{1, 2},
		Parent: cgroupDirectoryID{Major: 0, Minor: 1, Inode: 1, Mount: 1},
		Group:  cgroupDirectoryID{Major: 0, Minor: 1, Inode: 2, Mount: 1},
	})
	if err != nil {
		fuzzer.Fatal(err)
	}
	fuzzer.Add(seed)
	fuzzer.Fuzz(func(t *testing.T, encoded []byte) {
		identity, err := decodeServiceRecoveryIdentity(encoded)
		if err != nil {
			if identity != (serviceRecoveryIdentity{}) {
				t.Fatal("failed decoding returned authority")
			}
			return
		}
		canonical, err := json.Marshal(identity)
		if err != nil || !bytes.Equal(encoded, canonical) {
			t.Fatal("accepted noncanonical identity:", err)
		}
	})
}
