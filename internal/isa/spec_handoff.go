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

package isa

// HandOffKind is what an operation does at one hand-off route.
type HandOffKind uint8

const (
	// handOffUndeclared is the zero value and means that the row declares no hand-off
	// behaviour.
	handOffUndeclared HandOffKind = iota

	// handOffNone stores no reference outside the operation's own registers and arena-owned
	// storage, or stores only pointer-free data.
	handOffNone

	// HandOffCopy copies what Go copies (string bytes, by-value structs and arrays, scalar
	// boxes) before the store, so no arena reference leaves through the copied part.
	HandOffCopy

	// HandOffRevoke reaches a hand-off site that, in a licensed arena, revokes the arena
	// storage the stored value references.
	HandOffRevoke

	// HandOffBorrow passes values to a native callee whose borrow class determines the
	// hand-off.
	HandOffBorrow

	// HandOffGrant keeps a value in the arena, or skips a store barrier, on a confinement
	// grant the escape pass carries for the instruction; licensed reuse relies on the escape
	// pass's grants being correct.
	HandOffGrant

	// numHandOffKinds is the number of kinds.
	numHandOffKinds
)

// HandOffRoute is a route from a guest value to storage that may outlive the arena.
type HandOffRoute uint8

const (
	// RouteNone is the route of a handOffNone entry.
	RouteNone HandOffRoute = iota

	// RouteHostReturn is a value returned to the host.
	RouteHostReturn

	// RouteContainerStore is a store into a host field or map, or into any container the
	// arena's mark does not trace.
	RouteContainerStore

	// RouteFieldAppend is an append through a pointer into a slice the arena does not own,
	// such as a host struct's field.
	RouteFieldAppend

	// routeGlobalStore is a store into a package global.
	routeGlobalStore

	// routeNativeArgument is an argument passed to a native function or method.
	routeNativeArgument

	// routeChannelSend is a value sent on a channel.
	routeChannelSend

	// routeSpawn is a goroutine's arguments and captures.
	routeSpawn

	// routeClosureCapture is a value captured by a closure, deferred call or bound method.
	routeClosureCapture

	// routeCopyBuiltin is the destination of the copy builtin.
	routeCopyBuiltin

	// routeAppendHostBacking is an append into a backing the arena does not own.
	routeAppendHostBacking

	// routePanicCapture is a panic value captured for recover or reported to the host.
	routePanicCapture

	// routeSharedStore is a store into a captured variable while another goroutine shares
	// it.
	routeSharedStore

	// RouteArenaInternal is a store in arena-owned storage or a map only the VM can reach.
	RouteArenaInternal

	// numHandOffRoutes is the number of routes.
	numHandOffRoutes
)

// handOffOutcome is what the licensed-reuse design requires at a route.
type handOffOutcome uint8

const (
	// outcomeNone requires nothing.
	outcomeNone handOffOutcome = iota

	// outcomeCopyThenRevoke copies what Go copies and revokes what the rest references.
	outcomeCopyThenRevoke

	// outcomeRevoke revokes what the value references.
	outcomeRevoke

	// outcomeByClass applies the callee's borrow class.
	outcomeByClass

	// outcomeDischarge leaves the licence as it was, on a grant.
	outcomeDischarge
)

// handOffGranularity is the storage a revocation at a route covers.
type handOffGranularity uint8

const (
	// granularityNone revokes nothing.
	granularityNone handOffGranularity = iota

	// granularityStorageReached revokes the chunks and slabs the value reaches.
	granularityStorageReached

	// granularityStorageReachedEpochPastBudget revokes the storage reached, or the whole
	// epoch once the walk's budget is spent, counting a blind hand-off as well.
	granularityStorageReachedEpochPastBudget

	// granularityStorageReachedEpochWhenUnsure revokes the storage reached, or the whole
	// epoch when the walk cannot tell where a reference lands.
	granularityStorageReachedEpochWhenUnsure

	// granularityEpoch revokes the whole epoch.
	granularityEpoch
)

// HandOffEntry is one hand-off an operation's handler makes.
type HandOffEntry struct {
	// Causes are the hand-off causes a revoke or borrow entry's sites count under.
	Causes []HandOffCause

	// Kind is what the handler does.
	Kind HandOffKind

	// Route is where the value goes.
	Route HandOffRoute
}

// handOffRouteSpec records the permitted operation kinds, licensed-reuse requirements and
// revocation coverage for one hand-off route.
type handOffRouteSpec struct {
	// Rule names the rule changing the route's granularity, or "" when none applies.
	Rule string

	// Name is the route's stable name.
	Name string

	// Kinds are the entry kinds an operation may declare on the route.
	Kinds []HandOffKind

	// Route is the route.
	Route HandOffRoute

	// Outcome is what the design requires.
	Outcome handOffOutcome

	// Granularity is what a revocation covers.
	Granularity handOffGranularity

	// RuleGranularity is what a revocation covers while Rule applies, or granularityNone
	// when no rule changes the route.
	RuleGranularity handOffGranularity
}

// handOffRuleClosureCellWalk is the rule under which a closure's hand-off walks its
// upvalue cells, so closure-capture revokes the storage reached instead of the epoch.
const handOffRuleClosureCellWalk = "closure-cell-walk"

// HandOffBoundary records a hand-off outside an individual operation handler.
type HandOffBoundary struct {
	// Name is the boundary's stable name.
	Name string

	// Roots name the engine functions that enter the boundary.
	Roots []string

	// HandOffs are the boundary's entries.
	HandOffs []HandOffEntry
}

// handOffKindNames holds the stable names of the kinds.
var handOffKindNames = [numHandOffKinds]string{
	handOffUndeclared: "undeclared",
	handOffNone:       "none",
	HandOffCopy:       "copy",
	HandOffRevoke:     "revoke",
	HandOffBorrow:     "borrow",
	HandOffGrant:      "grant",
}

// copyThenRevoke lists the kinds a copy-then-revoke route admits.
var copyThenRevoke = []HandOffKind{HandOffCopy, HandOffRevoke}

// handOffRoutes is the route table, indexed by HandOffRoute. A copy-then-revoke route is
// one where the handlers copy what Go copies before the store (some may only revoke); a
// revoke route is one where no handler copies, so the stored value keeps its arena string
// bytes and the walk revokes them.
var handOffRoutes = [numHandOffRoutes]handOffRouteSpec{
	RouteNone: {Name: "none", Route: RouteNone, Outcome: outcomeNone, Granularity: granularityNone, Kinds: []HandOffKind{handOffNone}, RuleGranularity: 0, Rule: ""},
	RouteHostReturn: {Name: "host-return", Route: RouteHostReturn, Outcome: outcomeCopyThenRevoke, Granularity: granularityStorageReachedEpochPastBudget, Kinds: copyThenRevoke,
		RuleGranularity: 0, Rule: ""},
	RouteContainerStore: {Name: "container-store", Route: RouteContainerStore, Outcome: outcomeCopyThenRevoke, Granularity: granularityStorageReachedEpochPastBudget,
		Kinds: copyThenRevoke, RuleGranularity: 0, Rule: ""},
	RouteFieldAppend: {Name: "field-append", Route: RouteFieldAppend, Outcome: outcomeRevoke, Granularity: granularityStorageReachedEpochPastBudget,
		Kinds: []HandOffKind{HandOffRevoke}, RuleGranularity: 0, Rule: ""},
	routeGlobalStore: {Name: "global-store", Route: routeGlobalStore, Outcome: outcomeCopyThenRevoke, Granularity: granularityStorageReachedEpochPastBudget, Kinds: copyThenRevoke,
		RuleGranularity: 0, Rule: ""},
	routeNativeArgument: {Name: "native-argument", Route: routeNativeArgument, Outcome: outcomeByClass, Granularity: granularityStorageReachedEpochWhenUnsure,
		Kinds: []HandOffKind{HandOffBorrow, HandOffCopy, HandOffRevoke}, RuleGranularity: 0, Rule: ""},
	routeChannelSend: {Name: "channel-send", Route: routeChannelSend, Outcome: outcomeCopyThenRevoke, Granularity: granularityStorageReachedEpochPastBudget, Kinds: copyThenRevoke,
		RuleGranularity: 0, Rule: ""},
	routeSpawn: {Name: "spawn", Route: routeSpawn, Outcome: outcomeCopyThenRevoke, Granularity: granularityStorageReachedEpochPastBudget, Kinds: copyThenRevoke,
		RuleGranularity: 0, Rule: ""},
	routeClosureCapture: {Name: "closure-capture", Route: routeClosureCapture, Outcome: outcomeRevoke, Granularity: granularityEpoch, Kinds: copyThenRevoke,
		RuleGranularity: granularityStorageReached, Rule: handOffRuleClosureCellWalk},
	routeCopyBuiltin: {Name: "copy-builtin", Route: routeCopyBuiltin, Outcome: outcomeRevoke, Granularity: granularityStorageReachedEpochPastBudget,
		Kinds: []HandOffKind{HandOffRevoke}, RuleGranularity: 0, Rule: ""},
	routeAppendHostBacking: {Name: "append-host-backing", Route: routeAppendHostBacking, Outcome: outcomeCopyThenRevoke, Granularity: granularityStorageReachedEpochPastBudget,
		Kinds: copyThenRevoke, RuleGranularity: 0, Rule: ""},
	routePanicCapture: {Name: "panic-capture", Route: routePanicCapture, Outcome: outcomeCopyThenRevoke, Granularity: granularityStorageReachedEpochPastBudget, Kinds: copyThenRevoke,
		RuleGranularity: 0, Rule: ""},
	routeSharedStore: {Name: "shared-store", Route: routeSharedStore, Outcome: outcomeCopyThenRevoke, Granularity: granularityStorageReachedEpochPastBudget, Kinds: copyThenRevoke,
		RuleGranularity: 0, Rule: ""},
	RouteArenaInternal: {Name: "arena-internal", Route: RouteArenaInternal, Outcome: outcomeDischarge, Granularity: granularityNone, Kinds: []HandOffKind{HandOffGrant},
		RuleGranularity: 0, Rule: ""},
}

// nativeArgumentCauses are the four causes a native argument is counted under, one per
// borrow class.
var nativeArgumentCauses = []HandOffCause{
	HandOffNativeArgUnknown, HandOffNativeArgRetains, HandOffNativeArgBorrows, HandOffNativeArgResultAliases,
}

// escapeCopyCauses lists catch-all causes for an untraced store, a heap slice passed
// through and a reference kept by identity during escape copying.
var escapeCopyCauses = []HandOffCause{HandOffUntracedContainer, HandOffHeapPassthrough, HandOffIdentityKept}

// handOffBoundaries are the hand-offs made outside any one handler.
var handOffBoundaries = []HandOffBoundary{
	{
		Name:  "host-return",
		Roots: []string{"MaterialiseAnyForArena"},
		HandOffs: []HandOffEntry{
			{Kind: HandOffCopy, Route: RouteHostReturn, Causes: nil},
			{Kind: HandOffRevoke, Route: RouteHostReturn, Causes: []HandOffCause{HandOffHeapPassthrough, HandOffIdentityKept}},
		},
	},
	{
		Name:  "host-arguments",
		Roots: []string{"MaterialiseHostArguments"},
		HandOffs: []HandOffEntry{
			{Kind: HandOffCopy, Route: RouteContainerStore, Causes: nil},
			{Kind: HandOffRevoke, Route: RouteContainerStore, Causes: []HandOffCause{HandOffWalkExhausted, HandOffNotSettable, HandOffIdentityKept, HandOffHeapPassthrough}},
		},
	},
	{
		Name:  "callback-return",
		Roots: []string{"materialiseCallbackResults"},
		HandOffs: []HandOffEntry{
			{Kind: HandOffCopy, Route: RouteHostReturn, Causes: nil},
			{Kind: HandOffRevoke, Route: RouteHostReturn, Causes: []HandOffCause{HandOffHeapPassthrough, HandOffIdentityKept}},
		},
	},
	{
		Name:  "callback-host-arguments",
		Roots: []string{"materialiseCallbackArguments"},
		HandOffs: []HandOffEntry{
			{Kind: HandOffCopy, Route: RouteContainerStore, Causes: nil},
			{Kind: HandOffRevoke, Route: RouteContainerStore, Causes: []HandOffCause{HandOffWalkExhausted, HandOffNotSettable, HandOffIdentityKept, HandOffHeapPassthrough}},
		},
	},
	{
		Name:  "global-release",
		Roots: []string{"VM.MaterialiseGlobalStrings"},
		HandOffs: []HandOffEntry{
			{Kind: HandOffCopy, Route: routeGlobalStore, Causes: nil},
			{Kind: HandOffRevoke, Route: routeGlobalStore, Causes: []HandOffCause{HandOffHeapPassthrough, HandOffIdentityKept}},
		},
	},
	{
		Name:  "panic-capture",
		Roots: []string{"VM.recordRecoveredPanic", "raiseNativePanicAsInterpreted"},
		HandOffs: []HandOffEntry{
			{Kind: HandOffCopy, Route: routePanicCapture, Causes: nil},
			{Kind: HandOffRevoke, Route: routePanicCapture, Causes: escapeCopyCauses},
		},
	},
}

// reentryFunctions name the engine functions that run guest instructions again (a
// callback, a deferred call).
var reentryFunctions = []string{"VM.runDispatched", "VM.runDispatchedLoop", "VM.run", "flatDispatchSwitch", "VM.unwindAfterAbort"}

// String returns the kind's stable name.
//
// Returns string which is the name, or "unknown" when out of range.
func (k HandOffKind) String() string {
	if k >= numHandOffKinds {
		return "unknown"
	}
	return handOffKindNames[k]
}

// String returns the route's stable name.
//
// Returns string which is the name, or "unknown" when out of range.
func (r HandOffRoute) String() string {
	if r >= numHandOffRoutes {
		return "unknown"
	}
	return handOffRoutes[r].Name
}

// String returns the outcome's stable name.
//
// Returns string which is the name.
func (o handOffOutcome) String() string {
	switch o {
	case outcomeCopyThenRevoke:
		return "copy-then-revoke"
	case outcomeRevoke:
		return "revoke"
	case outcomeByClass:
		return "by-class"
	case outcomeDischarge:
		return "discharge"
	default:
		return "none"
	}
}

// String returns the granularity's stable name.
//
// Returns string which is the name.
func (g handOffGranularity) String() string {
	switch g {
	case granularityStorageReached:
		return "storage-reached"
	case granularityStorageReachedEpochPastBudget:
		return "storage-reached/epoch-past-budget"
	case granularityStorageReachedEpochWhenUnsure:
		return "storage-reached/epoch-when-unsure"
	case granularityEpoch:
		return "epoch"
	default:
		return "none"
	}
}

// handOffRouteList returns the route table in route order.
//
// Returns []handOffRouteSpec which is a copy.
func handOffRouteList() []handOffRouteSpec {
	return append([]handOffRouteSpec(nil), handOffRoutes[:]...)
}

// handOffRoutesUnder returns the route table with the granularity each route has while
// its rule applies.
//
// Takes closureCellWalk (bool) which is true while closure hand-offs walk cells.
//
// Returns []handOffRouteSpec which is a copy.
func handOffRoutesUnder(closureCellWalk bool) []handOffRouteSpec {
	routes := handOffRouteList()
	for index := range routes {
		if closureCellWalk && routes[index].Rule == handOffRuleClosureCellWalk {
			routes[index].Granularity = routes[index].RuleGranularity
		}
	}
	return routes
}

// HandOffBoundaries returns the boundaries.
//
// Returns []HandOffBoundary which is a copy.
func HandOffBoundaries() []HandOffBoundary {
	return append([]HandOffBoundary(nil), handOffBoundaries...)
}

// HandOffReentryFunctions returns the engine functions that re-enter dispatch.
//
// Returns []string which is a copy.
func HandOffReentryFunctions() []string {
	return append([]string(nil), reentryFunctions...)
}

// RequiresHandOff reports whether the row must declare its hand-offs.
//
// It mutates memory beyond its registers, calls, or publishes a value.
//
// Returns bool which is true for such a row.
func (s OpSpec) RequiresHandOff() bool {
	if s.Has(specMeta) || s.Has(specReserved) {
		return false
	}
	return s.Has(specMutatesMemory) || s.Has(specCalls) || s.Has(specPublishes)
}

// Calls reports whether the operation transfers control to a callee.
//
// Returns bool which is true when specCalls is set.
func (s OpSpec) Calls() bool { return s.Has(specCalls) }

// publishesValue reports whether a return, panic or send makes a value visible beyond the
// VM.
//
// Returns bool which is true when specPublishes is set.
func (s OpSpec) publishesValue() bool { return s.Has(specPublishes) }

// HandOffs returns the row's hand-off entries, and whether it declared any. A row that
// need not declare and did not is reported as one handOffNone entry.
//
// Returns entries ([]HandOffEntry) which is a copy.
// Returns declared (bool) which is false when the row is silent.
func (s OpSpec) HandOffs() (entries []HandOffEntry, declared bool) {
	if len(s.HandOff) != 0 {
		return append([]HandOffEntry(nil), s.HandOff...), true
	}
	if s.RequiresHandOff() || s.Handler == "" {
		return nil, false
	}
	return []HandOffEntry{{Kind: handOffNone, Route: RouteNone, Causes: nil}}, false
}

// DeclaredCauses returns the causes the row's revoke and borrow entries name.
//
// Returns map[HandOffCause]bool which holds them.
func (s OpSpec) DeclaredCauses() map[HandOffCause]bool {
	causes := map[HandOffCause]bool{}
	for _, entry := range s.HandOff {
		for _, cause := range entry.Causes {
			causes[cause] = true
		}
	}
	return causes
}

// HasHandOffKind reports whether the row declares an entry of kind.
//
// Takes kind (HandOffKind) which is the kind to look for.
//
// Returns bool which is true when one is declared.
func (s OpSpec) HasHandOffKind(kind HandOffKind) bool {
	for _, entry := range s.HandOff {
		if entry.Kind == kind {
			return true
		}
	}
	return false
}

// calls marks the operation as transferring control to a callee.
//
// Returns OpSpec with specCalls set.
func (s OpSpec) calls() OpSpec { s.Flags |= specCalls; return s }

// publishes marks the operation as making a value visible beyond the VM.
//
// Returns OpSpec with specPublishes set.
func (s OpSpec) publishes() OpSpec { s.Flags |= specPublishes; return s }

// handOff appends one entry to the row's hand-off column.
//
// Takes kind (HandOffKind) which is what the handler does.
// Takes route (HandOffRoute) which is where the value goes.
// Takes causes (...HandOffCause) which the entry's sites count under.
//
// Returns OpSpec with the entry appended.
func (s OpSpec) handOff(kind HandOffKind, route HandOffRoute, causes ...HandOffCause) OpSpec {
	entries := make([]HandOffEntry, len(s.HandOff), len(s.HandOff)+1)
	copy(entries, s.HandOff)
	entries = append(entries, HandOffEntry{Kind: kind, Route: route, Causes: causes})
	s.HandOff = entries
	return s
}

// noHandOff declares that the operation hands nothing off.
//
// Returns OpSpec with a handOffNone entry.
func (s OpSpec) noHandOff() OpSpec { return s.handOff(handOffNone, RouteNone) }

// copies declares a copy of what Go copies on route.
//
// Takes route (HandOffRoute) which is where the copy goes.
//
// Returns OpSpec with a HandOffCopy entry.
func (s OpSpec) copies(route HandOffRoute) OpSpec { return s.handOff(HandOffCopy, route) }

// revokes declares hand-off sites of the given causes on route.
//
// Takes route (HandOffRoute) which is where the value goes.
// Takes causes (...HandOffCause) which the sites count under.
//
// Returns OpSpec with a HandOffRevoke entry.
func (s OpSpec) revokes(route HandOffRoute, causes ...HandOffCause) OpSpec {
	return s.handOff(HandOffRevoke, route, causes...)
}

// copiesThenRevokes declares a copy of what Go copies on route, followed by the escape
// copy's catch-all hand-offs.
//
// Takes route (HandOffRoute) which is where the value goes.
// Takes causes (...HandOffCause) which the catch-alls count under.
//
// Returns OpSpec with a HandOffCopy and a HandOffRevoke entry.
func (s OpSpec) copiesThenRevokes(route HandOffRoute, causes ...HandOffCause) OpSpec {
	return s.copies(route).revokes(route, causes...)
}

// borrows declares native arguments classed by the borrow registry.
//
// Returns OpSpec with a HandOffBorrow entry on routeNativeArgument.
func (s OpSpec) borrows() OpSpec {
	return s.handOff(HandOffBorrow, routeNativeArgument, nativeArgumentCauses...)
}

// grants declares that the handler consults an escape-pass grant.
//
// Returns OpSpec with a HandOffGrant entry on RouteArenaInternal.
func (s OpSpec) grants() OpSpec { return s.handOff(HandOffGrant, RouteArenaInternal) }
