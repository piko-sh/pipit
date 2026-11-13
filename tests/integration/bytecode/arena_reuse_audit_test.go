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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
)

var auditScalarPrograms = []struct {
	name   string
	source string
	want   any
}{
	{name: "string concatenation", source: `
func f() int {
	s := ""
	for i := 0; i < 100; i++ {
		s += "ab"
	}
	return len(s)
}
f()`, want: 200},
	{name: "slice append", source: `
func f() int {
	xs := make([]int, 0)
	for i := 0; i < 1000; i++ {
		xs = append(xs, i)
	}
	t := 0
	for _, x := range xs {
		t += x
	}
	return t
}
f()`, want: 499500},
	{name: "string-keyed map", source: `
func f() int {
	m := map[string]int{}
	for i := 0; i < 50; i++ {
		m[string(rune('a'+i%26))+"k"] = i
	}
	return len(m)
}
f()`, want: 26},
	{name: "closure capture", source: `
func f() int {
	acc := 0
	add := func(n int) { acc += n }
	for i := 0; i < 10; i++ {
		add(i)
	}
	return acc
}
f()`, want: 45},
	{name: "struct with string fields", source: `
type P struct {
	N string
	A int
}
func f() int {
	ps := []P{}
	for i := 0; i < 100; i++ {
		ps = append(ps, P{N: "x" + "y", A: i})
	}
	t := 0
	for _, p := range ps {
		t += p.A + len(p.N)
	}
	return t
}
f()`, want: 5150},
	{name: "float accumulation", source: `
func f() float64 {
	x := 0.0
	for i := 0; i < 10; i++ {
		x += 0.5
	}
	return x
}
f()`, want: 5.0},
	{name: "minor GC churn", source: arenaRecyclingChurn, want: 97920 + 2016 + 15},
}

func TestAuditScalarResultProgramsAreSilent(t *testing.T) {
	for _, hit := range engine.AuditReuse(engine.AuditOptions{}) {
		t.Logf("pipit-audit: hit left by an earlier test: %s", hit)
	}
	previous := engine.AuditSetMidRunEvery(1)
	t.Cleanup(func() { engine.AuditSetMidRunEvery(previous) })
	before := engine.AuditStats()

	service := newTestService(t)
	for _, program := range auditScalarPrograms {
		for range 3 {
			result, err := service.Eval(t.Context(), program.source)
			require.NoError(t, err, program.name)
			require.EqualValues(t, program.want, result, program.name)
		}
	}

	hits := engine.AuditReuse(engine.AuditOptions{})
	after := engine.AuditStats()
	require.Greater(t, after.Recorded, before.Recorded, "the programs' arena releases must be recorded")
	require.Greater(t, after.MidRunChecks, before.MidRunChecks, "the churn program must reach a mid-run checkpoint")
	require.Equal(t, before.ReleaseHits, after.ReleaseHits, "scalar-result programs must leave no released arena memory reachable")
	require.Equal(t, before.OutsideReachHits, after.OutsideReachHits, "no slab MinorGC found dead may outlive the run")
	t.Logf("pipit-audit: %d in-run-unrooted hits across %d mid-run checks, all resolved in-run-only",
		after.InRunUnrootedHits-before.InRunUnrootedHits, after.MidRunChecks-before.MidRunChecks)
	for _, hit := range hits {
		require.Equal(t, engine.AuditInRunOnly, hit.Class, "%s", hit)
		require.Positive(t, hit.ArenaHeaderRefs, "a mid-run hit must be explained by the arena's stale slice headers: %s", hit)
	}
}
