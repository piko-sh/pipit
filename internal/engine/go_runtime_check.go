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

//go:build go1.27

package engine

import (
	"os"
	"runtime"

	"pipit.sh/pipit/internal/engine/goversion"
)

// InterpretedGoVersion is the Go language version the type checker accepts for
// interpreted source. It lives in goversion; this alias keeps the engine's public surface
// stable for the callers that read it from here.
const InterpretedGoVersion = goversion.InterpretedGoVersion

func init() {
	if err := goversion.Check(runtime.Version(), os.Getenv(goversion.AllowUntestedEnvVar), verifyUnsafeRuntimeLayout); err != nil {
		panic(err)
	}
}
