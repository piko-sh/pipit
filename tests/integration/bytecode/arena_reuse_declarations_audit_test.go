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

//go:build integration && pipit_reuse_audit && !safe && !(js && wasm)

package bytecode_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
)

const dishonestReadsOnlySource = `package main

import (
	"declhost"
	"strconv"
)

func mk(n int) string { return "decl-" + strconv.Itoa(n) + "-" + strconv.Itoa(n*7) + "-tail" }

func Lie(n int) string {
	s := mk(n)
	k := declhost.Scribble([]byte(s))
	return s + "/" + strconv.Itoa(k)
}

func Churn(n int) int { return n }
`

func TestDishonestReadsOnlyDeclarationIsCaughtByTheAudit(t *testing.T) {
	engine.ResetReadsOnlyAuditForTest()
	t.Cleanup(engine.ResetReadsOnlyAuditForTest)
	window := openAuditWindow(t, 0)
	host := &declarationHost{}
	service := declarationService(t, host)
	require.NoError(t, service.DeclareNativeBorrowClass(hostScribble, engine.BorrowReadsOnly),
		"the lie passes the declaration checks; only the audit can catch it")
	compiled := hazardCompile(t, service, dishonestReadsOnlySource)
	observations := &hazardObservations{}
	before := engine.LicenceDiagnostics().HostDeclarations
	got := make([]string, 0, declarationRounds)
	for round := range declarationRounds {
		got = append(got, fmt.Sprint(hazardCall(t, service, compiled, "Lie", 10*round+1)))
	}
	stats := engine.LicenceDiagnostics().HostDeclarations
	reuseDeclarationArenas(t, service, compiled)
	for round, value := range got {
		n := 10*round + 1
		observations.compareString(fmt.Sprintf("Lie(%d)", n), declarationMk(n)+"/"+fmt.Sprint(len(declarationMk(n))), value)
	}
	violations := engine.ReadsOnlyViolations()
	observations.note("reads-only audit: violations=%v views=%d fallbacks=%d", violations, stats.Views-before.Views, stats.ViewFallbacks-before.ViewFallbacks)
	if !slices.ContainsFunc(violations, func(v string) bool { return strings.Contains(v, "hostScribble") }) {
		observations.fire("reads-only-violation-missed", "the writing reads-only native was not reported")
	}
	if fallbacks := stats.ViewFallbacks - before.ViewFallbacks; fallbacks < declarationRounds-1 {
		observations.fire("reads-only-not-demoted", "%d of %d later calls took the copying path", fallbacks, declarationRounds-1)
	}
	finishDeclarationCase(t, observations, window)
}
