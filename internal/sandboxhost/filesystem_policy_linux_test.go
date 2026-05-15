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

package sandboxhost

import (
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFilesystemRecoveryPolicyCoversConfiguration(t *testing.T) {
	directory := t.TempDir()
	config := IsolatedFilesystemConfig{
		BrokerPath:   filepath.Join(directory, "broker"),
		BrokerSHA256: [sha256.Size]byte{1},
		Roots:        []FilesystemRoot{{Name: "data", HostPath: filepath.Join(directory, "data"), Rights: FilesystemRead}},
		Worker: IsolatedConfig{Tenant: "test-tenant", StateDirectory: filepath.Join(directory, "state"),
			WorkerPath:        filepath.Join(directory, "worker"),
			WatchdogPath:      filepath.Join(directory, "watchdog"),
			LinuxCgroupParent: filepath.Join(directory, "cgroup"),
			WorkerSHA256:      [sha256.Size]byte{1},
			WatchdogSHA256:    [sha256.Size]byte{1},
			Imports:           []string{"math"},
		},
	}
	original, err := isolatedFilesystemRecoveryPolicy(context.Background(), &config)
	if err != nil || original == ([sha256.Size]byte{}) {
		t.Fatal("invalid baseline:", err)
	}
	mutations := map[string]func(*IsolatedFilesystemConfig){
		"BrokerPath":               func(config *IsolatedFilesystemConfig) { config.BrokerPath += "-other" },
		"Worker.Tenant":            func(config *IsolatedFilesystemConfig) { config.Worker.Tenant += "-other" },
		"Worker.StateDirectory":    func(config *IsolatedFilesystemConfig) { config.Worker.StateDirectory += "-other" },
		"BrokerSHA256":             func(config *IsolatedFilesystemConfig) { config.BrokerSHA256[1] = 1 },
		"Roots.Name":               func(config *IsolatedFilesystemConfig) { config.Roots[0].Name = "other" },
		"Roots.HostPath":           func(config *IsolatedFilesystemConfig) { config.Roots[0].HostPath += "-other" },
		"Roots.Rights":             func(config *IsolatedFilesystemConfig) { config.Roots[0].Rights |= FilesystemWrite },
		"Worker.WorkerPath":        func(config *IsolatedFilesystemConfig) { config.Worker.WorkerPath += "-other" },
		"Worker.WatchdogPath":      func(config *IsolatedFilesystemConfig) { config.Worker.WatchdogPath += "-other" },
		"Worker.LinuxCgroupParent": func(config *IsolatedFilesystemConfig) { config.Worker.LinuxCgroupParent += "-other" },
		"Worker.Imports":           func(config *IsolatedFilesystemConfig) { config.Worker.Imports = nil },
		"Worker.WorkerSHA256":      func(config *IsolatedFilesystemConfig) { config.Worker.WorkerSHA256[1] = 1 },
		"Worker.WatchdogSHA256":    func(config *IsolatedFilesystemConfig) { config.Worker.WatchdogSHA256[1] = 1 },
		"Worker.MemoryBytes":       func(config *IsolatedFilesystemConfig) { config.Worker.MemoryBytes = 128 << 20 },
		"Worker.CPUMilli":          func(config *IsolatedFilesystemConfig) { config.Worker.CPUMilli = 500 },
		"Worker.Tasks":             func(config *IsolatedFilesystemConfig) { config.Worker.Tasks = 32 },
		"Worker.Lifetime":          func(config *IsolatedFilesystemConfig) { config.Worker.Lifetime = 1 },
		"Worker.OutputBytes":       func(config *IsolatedFilesystemConfig) { config.Worker.OutputBytes = 1 },
		"Limits.Calls":             func(config *IsolatedFilesystemConfig) { config.Limits.Calls = 32 },
		"Limits.Outstanding":       func(config *IsolatedFilesystemConfig) { config.Limits.Outstanding = 1 },
		"Limits.ReadBytes":         func(config *IsolatedFilesystemConfig) { config.Limits.ReadBytes = 1 },
		"Limits.WriteBytes":        func(config *IsolatedFilesystemConfig) { config.Limits.WriteBytes = 1 },
		"Limits.ListEntries":       func(config *IsolatedFilesystemConfig) { config.Limits.ListEntries = 1 },
	}
	assertRecoveryPolicyFields(t, reflect.TypeFor[IsolatedFilesystemConfig](), "", mutations)
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			copied := config
			copied.Roots = append([]FilesystemRoot(nil), config.Roots...)
			mutate(&copied)
			changed, err := isolatedFilesystemRecoveryPolicy(context.Background(), &copied)
			if err != nil || changed == original || changed == ([sha256.Size]byte{}) {
				t.Fatal("policy change did not invalidate approval:", err)
			}
		})
	}
	repeated, err := isolatedFilesystemRecoveryPolicy(context.Background(), &config)
	if err != nil || repeated != original {
		t.Fatal("policy mutated caller state or produced unstable approval:", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if digest, err := isolatedFilesystemRecoveryPolicy(ctx, &config); !errors.Is(err, context.Canceled) || digest != ([sha256.Size]byte{}) {
		t.Fatal("cancelled policy produced approval:", err)
	}
	if digest, err := isolatedFilesystemRecoveryPolicy(context.Background(), nil); !errors.Is(err, ErrInvalidIsolatedConfig) || digest != ([sha256.Size]byte{}) {
		t.Fatal("missing policy produced approval:", err)
	}
	config.Worker.Tenant = ""
	if digest, err := isolatedFilesystemRecoveryPolicy(context.Background(), &config); !errors.Is(err, ErrInvalidIsolatedConfig) || digest != ([sha256.Size]byte{}) {
		t.Fatal("invalid policy produced approval:", err)
	}
}

func assertRecoveryPolicyFields(t *testing.T, policy reflect.Type, prefix string, mutations map[string]func(*IsolatedFilesystemConfig)) {
	t.Helper()
	for field := range policy.Fields() {
		name := prefix + field.Name
		if field.Tag.Get("json") == "-" {

			continue
		}
		nested := field.Type
		if nested.Kind() == reflect.Slice && nested.Elem().Kind() == reflect.Struct {
			nested = nested.Elem()
		}
		if nested.Kind() == reflect.Struct {
			assertRecoveryPolicyFields(t, nested, name+".", mutations)
		} else if mutations[name] == nil {
			t.Errorf("configuration field lacks an approval-invalidation regression: %s", name)
		}
	}
}
