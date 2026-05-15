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

//go:build integration && fuzz

package bytecode_test

import (
	"context"
	"testing"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/verify"
)

func FuzzVerifier(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0, 0, 0, 0})
	f.Add([]byte{byte(isa.OpAddInt), 1, 1, 2})
	f.Add([]byte{
		byte(isa.OpLoadIntConst), 0, 0, 0,
		byte(isa.OpAddInt), 1, 0, 0,
		byte(isa.OpDrillTier1), byte(isa.SubOpDrillTier2), byte(isa.SubOpTier2Return), 1,
	})
	f.Add(make([]byte, 64))

	f.Fuzz(func(t *testing.T, raw []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("verifier panicked: %v", r)
			}
		}()
		body := decodeFuzzBody(raw)
		compiledFunction := &program.CompiledFunction{
			Name: "fuzz",
			Body: body,
		}
		report, err := verify.VerifyBytecode(context.Background(), compiledFunction)
		if report == nil {
			t.Fatalf("verifyBytecode returned nil report (err=%v)", err)
		}
		_ = report.Format()
	})
}
