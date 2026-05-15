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

package codec_test

import (
	"maps"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine/program"
)

const unreviewedNotSerialisedMarker = "see TestUnreviewedNotSerialisedFieldsAreTracked"

const unreviewedNotSerialisedBudget = 26

func TestCompiledFunctionFieldCensus(t *testing.T) {
	t.Parallel()
	requireEveryFieldDeclared(t, reflect.TypeFor[program.CompiledFunction](),
		compiledFunctionSerialised, compiledFunctionNotSerialised)
}

func TestCallSiteFieldCensus(t *testing.T) {
	t.Parallel()
	requireEveryFieldDeclared(t, reflect.TypeFor[program.CallSite](),
		callSiteSerialised, callSiteNotSerialised)
}

func TestUnreviewedNotSerialisedFieldsAreTracked(t *testing.T) {
	t.Parallel()

	shared := make([]string, 0)
	for name := range callSiteNotSerialised {
		if _, both := compiledFunctionNotSerialised[name]; both {
			shared = append(shared, name)
		}
	}
	sort.Strings(shared)
	require.Emptyf(t, shared,
		"field names claimed by both notSerialised maps: %v\n"+
			"This test merges the two maps to look up a reason by name, so a shared name\n"+
			"would silently hide one of the two reasons. Give the census a per-type lookup\n"+
			"before introducing one.", shared)

	reasons := make(map[string]string, len(compiledFunctionNotSerialised)+len(callSiteNotSerialised))
	maps.Copy(reasons, compiledFunctionNotSerialised)
	maps.Copy(reasons, callSiteNotSerialised)

	var stale, trackedButReviewed []string
	for name := range unreviewedNotSerialisedFields {
		switch reason, declared := reasons[name]; {
		case !declared:
			stale = append(stale, name)
		case !strings.Contains(reason, unreviewedNotSerialisedMarker):
			trackedButReviewed = append(trackedButReviewed, name)
		}
	}

	var untracked []string
	for name, reason := range reasons {
		if strings.Contains(reason, unreviewedNotSerialisedMarker) && !unreviewedNotSerialisedFields[name] {
			untracked = append(untracked, name)
		}
	}

	sort.Strings(stale)
	sort.Strings(trackedButReviewed)
	sort.Strings(untracked)

	require.Emptyf(t, stale,
		"unreviewedNotSerialisedFields names fields that no longer exist: %v\n"+
			"The field was renamed or removed without updating this set, so the review it\n"+
			"was waiting for can never happen. Drop the name and lower\n"+
			"unreviewedNotSerialisedBudget to match.", stale)
	require.Emptyf(t, trackedButReviewed,
		"unreviewedNotSerialisedFields names fields whose reason has since been written: %v\n"+
			"The reason no longer cites this test, so the review is done. Drop the name and\n"+
			"lower unreviewedNotSerialisedBudget to match.", trackedButReviewed)
	require.Emptyf(t, untracked,
		"fields carry the generic absent-from-the-format reason but are not tracked: %v\n"+
			"An absent field either explains itself, or it is listed here so the backlog\n"+
			"stays countable. Write the real reason, or add the name to\n"+
			"unreviewedNotSerialisedFields and raise unreviewedNotSerialisedBudget in the\n"+
			"same commit, so the rise is something a reviewer has to approve.", untracked)

	require.LessOrEqualf(t, len(unreviewedNotSerialisedFields), unreviewedNotSerialisedBudget,
		"the unreviewed backlog grew from %d to %d\n"+
			"This number is a ratchet. A field whose zero value silently changes behaviour\n"+
			"is exactly the bug the census exists to catch, so a new absent field explains\n"+
			"itself rather than joining the queue.",
		unreviewedNotSerialisedBudget, len(unreviewedNotSerialisedFields))
}

func requireEveryFieldDeclared(t *testing.T, structType reflect.Type, serialised, notSerialised map[string]string) {
	t.Helper()

	var undeclared, doubleDeclared []string
	for field := range structType.Fields() {
		if !field.IsExported() {
			continue
		}
		_, isSerialised := serialised[field.Name]
		_, isNotSerialised := notSerialised[field.Name]
		switch {
		case isSerialised && isNotSerialised:
			doubleDeclared = append(doubleDeclared, field.Name)
		case !isSerialised && !isNotSerialised:
			undeclared = append(undeclared, field.Name)
		}
	}
	sort.Strings(undeclared)
	sort.Strings(doubleDeclared)

	require.Emptyf(t, undeclared,
		"%s has exported fields that are neither declared serialised nor declared\n"+
			"deliberately absent: %v\n"+
			"A field the compiler writes and the VM reads, but that nobody packs, silently\n"+
			"becomes its zero value in every loaded bundle. Add it to the wire format, or\n"+
			"record in the notSerialised map why it does not belong there.",
		structType.Name(), undeclared)
	require.Emptyf(t, doubleDeclared,
		"%s fields claimed by both declarations: %v", structType.Name(), doubleDeclared)

	for name := range notSerialised {
		require.NotEmptyf(t, notSerialised[name], "%s.%s must carry its reason", structType.Name(), name)
	}
}
