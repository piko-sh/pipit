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

package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"runtime"
	"slices"
	"sync"
	"weak"

	"piko.sh/safeconv"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/logging"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

const (
	// maxBankSlots is the number of slots one GlobalStore bank can hold. Slot indices are
	// uint16 and the bank length must itself fit one.
	maxBankSlots = math.MaxUint16

	// defaultMaxLoadedNamedInterfaces is how many distinct interface names loaded bytecode
	// may register in one service unless WithMaxLoadedNamedInterfaces says otherwise.
	defaultMaxLoadedNamedInterfaces = 1 << 16
)

var (
	// ErrProgramUnloaded is returned when a program is run after UnloadCompiled, or after
	// Reset emptied the store that held its package variables.
	ErrProgramUnloaded = fmt.Errorf("%w: the program was unloaded", policy.ErrClosed)

	// ErrProgramNotLoaded is returned by UnloadCompiled for a file set this service did not
	// load from bytecode.
	ErrProgramNotLoaded = errors.New("the file set was not loaded by this service")

	// ErrNamedInterfaceLimit is returned when loaded bytecode would register more distinct
	// interface names than the service allows.
	ErrNamedInterfaceLimit = errors.New("loaded named interface limit reached")
)

// digestingBytecodeStore is a bytecode store that also reports a digest of the bytes it
// read, so loading the same bytecode again can reuse the slots of the earlier load.
type digestingBytecodeStore interface {
	// LoadCompiledFileSetWithDigest loads a compiled file set and reports the SHA-256 of the
	// stored bytes it was decoded from.
	//
	// Takes key (string) which identifies the stored file set.
	// Takes registry (*symtab.SymbolRegistry) which rebuilds its runtime types.
	//
	// Returns *program.CompiledFileSet which is the decoded file set.
	// Returns [sha256.Size]byte which is the digest of the stored bytes.
	// Returns error which reports a read or decoding failure.
	LoadCompiledFileSetWithDigest(ctx context.Context, key string, registry *symtab.SymbolRegistry) (*program.CompiledFileSet, [sha256.Size]byte, error)
}

// globalSlotRegion is one contiguous run of slots in one GlobalStore bank.
type globalSlotRegion struct {
	// start is the first slot of the run.
	start int

	// count is the number of slots in the run.
	count int
}

// globalReservation is the set of slots one loaded program holds, shared by every load of
// the same bytecode.
type globalReservation struct {
	// regions holds, per register kind, the slots the reservation owns, padding included.
	regions [program.NumGlobalRegisterKinds]globalSlotRegion

	// bases holds the per-kind amounts the program's operands are moved by.
	bases program.SlotAllocation

	// low holds the lowest slot the program's operands name per kind.
	low program.SlotAllocation

	// count holds the number of slots the program's operands span per kind.
	count program.SlotAllocation

	// digest is the SHA-256 of the bytecode the reservation was made for.
	digest [sha256.Size]byte

	// hasDigest reports whether digest was supplied by the store.
	hasDigest bool

	// handles counts the loaded file sets that share the reservation and are not unloaded.
	handles int

	// runs counts the executions currently running a program that holds the reservation.
	runs int
}

// loadedProgram records one file set LoadCompiled returned.
type loadedProgram struct {
	// reservation is the slots the file set's operands are moved into.
	reservation *globalReservation

	// unloaded is set by UnloadCompiled and Reset. A later run is refused.
	unloaded bool
}

// loadedGlobalRegistry tracks the slots loaded programs hold, the regions freed by
// unloads, and the interface names loads registered.
type loadedGlobalRegistry struct {
	// byDigest maps a bytecode digest to the reservation its loads share.
	byDigest map[[sha256.Size]byte]*globalReservation

	// programs maps each loaded file set to its record. Keys are weak so a dropped file set
	// can be collected.
	programs map[weak.Pointer[program.CompiledFileSet]]*loadedProgram

	// interfaceNames holds the qualified interface names loads registered.
	interfaceNames map[string]struct{}

	// free holds, per register kind, the zeroed regions unloads returned.
	free [program.NumGlobalRegisterKinds][]globalSlotRegion

	// mu guards every field and serialises checking and reserving slots.
	mu sync.Mutex
}

// WithMaxLoadedNamedInterfaces caps how many distinct interface names bytecode loaded by
// LoadCompiled and LoadModule may register in the service. The default is 65,536.
//
// Takes count (int) which is the maximum. It must be positive.
//
// Returns Option which applies the limit.
func WithMaxLoadedNamedInterfaces(count int) Option {
	return positiveLimitOption("WithMaxLoadedNamedInterfaces", count, func(c *serviceConfig) {
		c.maxLoadedNamedInterfaces = count
	})
}

// UnloadCompiled releases the package-variable slots of a file set LoadCompiled returned.
// A later run of the file set returns ErrProgramUnloaded.
//
// The slots return to the service once no run of the program is in progress and no other
// load of the same bytecode still holds them. They are zeroed before another program
// reuses them. Function values the program handed the host must not be called after it is
// unloaded. Unloading a file set twice is a no-op.
//
// Takes cfs (*CompiledFileSet) which is the loaded file set.
//
// Returns error which is ErrProgramNotLoaded when this service did not load cfs.
//
// Concurrent access is guarded by a mutex. Safe for concurrent use. It locks
// loadedGlobals.mu.
func (s *Service) UnloadCompiled(cfs *program.CompiledFileSet) error {
	if cfs == nil {
		return ErrProgramNotLoaded
	}
	registry := &s.loadedGlobals
	registry.mu.Lock()
	record := registry.programs[weak.Make(cfs)]
	if record == nil {
		registry.mu.Unlock()
		return ErrProgramNotLoaded
	}
	if record.unloaded {
		registry.mu.Unlock()
		return nil
	}
	record.unloaded = true
	if reservation := record.reservation; reservation != nil {
		reservation.handles--
		s.releaseIfIdleLocked(reservation)
	}
	registry.mu.Unlock()
	s.forgetFileSetRecords(cfs)
	return nil
}

// beginLoadedRun counts a run of a loaded program so its slots are not freed under it.
//
// Takes cfs (*CompiledFileSet) which is about to run.
//
// Returns func() which ends the run and must be called once.
// Returns error which is ErrProgramUnloaded when cfs was unloaded.
//
// Concurrent access is guarded by a mutex. The registry mutex guards run counts. The
// returned function uses sync.Once to end the run once.
func (s *Service) beginLoadedRun(cfs *program.CompiledFileSet) (func(), error) {
	registry := &s.loadedGlobals
	registry.mu.Lock()
	defer registry.mu.Unlock()
	record := registry.programs[weak.Make(cfs)]
	if record == nil {
		return func() {}, nil
	}
	if record.unloaded {
		return nil, ErrProgramUnloaded
	}
	reservation := record.reservation
	if reservation == nil {
		return func() {}, nil
	}
	reservation.runs++
	var once sync.Once
	return func() {
		once.Do(func() {
			registry.mu.Lock()
			defer registry.mu.Unlock()
			reservation.runs--
			s.releaseIfIdleLocked(reservation)
		})
	}, nil
}

// placeLoadedProgram checks a loaded file set's interfaces and slot claims, reserves its
// slots (or reuses those of an earlier load of the same bytecode), moves its operands
// into them, and registers its interfaces.
//
// Takes cfs (*CompiledFileSet) which is the verified, loaded file set.
// Takes digest (*[sha256.Size]byte) which is the bytecode's digest, or nil when the store
// does not report one.
//
// Returns error when the file's slot claim does not cover its operands, an interface
// conflicts with one already registered, or the slots or interface names run out.
func (s *Service) placeLoadedProgram(ctx context.Context, cfs *program.CompiledFileSet, digest *[sha256.Size]byte) error {
	low, count, err := loadedSlotExtent(cfs)
	if err != nil {
		return err
	}
	if err := context.Cause(ctx); err != nil {
		return err
	}
	skipped, err := s.placeLoadedProgramLocked(cfs, digest, low, count)
	if err != nil {
		return err
	}
	reportSkippedInterfaces(ctx, skipped)
	return nil
}

// placeLoadedProgramLocked does placeLoadedProgram's work under the registry lock.
//
// Takes cfs (*CompiledFileSet) which is the loaded file set.
// Takes digest (*[sha256.Size]byte) which is the bytecode's digest, or nil.
// Takes low (SlotAllocation) which is the lowest slot named per kind.
// Takes count (SlotAllocation) which is the slots needed per kind.
//
// Returns []string which are the conflicting interface names left as they were.
// Returns error when an interface conflicts or the slots or interface names run out.
//
// Safe for concurrent use. It locks loadedGlobals.mu.
func (s *Service) placeLoadedProgramLocked(cfs *program.CompiledFileSet, digest *[sha256.Size]byte, low, count program.SlotAllocation) ([]string, error) {
	used := usedInterfaceNames(cfs)
	registry := &s.loadedGlobals
	registry.mu.Lock()
	defer registry.mu.Unlock()
	identities, skipped, err := s.checkNamedInterfacesLocked(cfs.NamedInterfaces(), used)
	if err != nil {
		return nil, err
	}
	reservation := registry.reusableLocked(digest, low, count)
	if reservation == nil {
		reservation, err = s.reserveLocked(low, count)
		if err != nil {
			return nil, err
		}
		registry.rememberDigestLocked(reservation, digest)
	}
	reservation.handles++
	bases := reservation.bases
	program.SetGlobalBases(cfs.Root(), &bases)
	program.SetGlobalBases(cfs.VariableInitFunction(), &bases)
	s.registerNamedInterfacesLocked(identities)
	s.trackLoadedProgramLocked(cfs, reservation)
	return skipped, nil
}

// rememberDigestLocked records a new reservation under the digest of the bytecode it was
// made for, so a later load of the same bytecode shares it.
//
// Takes reservation (*globalReservation) which was just made.
// Takes digest (*[sha256.Size]byte) which is the digest, or nil when there is none.
//
// The caller must hold registry.mu.
func (registry *loadedGlobalRegistry) rememberDigestLocked(reservation *globalReservation, digest *[sha256.Size]byte) {
	if digest == nil {
		return
	}
	reservation.digest, reservation.hasDigest = *digest, true
	if registry.byDigest == nil {
		registry.byDigest = make(map[[sha256.Size]byte]*globalReservation)
	}
	registry.byDigest[*digest] = reservation
}

// placeLoadedModule reserves the slots a module bundle declares, after checking that the
// declaration covers every operand, and registers its interfaces. Module slots stay
// reserved for the life of the service.
//
// Takes cfs (*CompiledFileSet) which is the unpacked bundle.
//
// Returns program.SlotAllocation which is the per-kind bases of the reserved region.
// Returns error when the claim does not cover the operands, an interface conflicts, or
// the slots or interface names run out.
//
// Concurrent access is guarded by a mutex. The registry mutex serialises interface checks
// and slot reservations.
func (s *Service) placeLoadedModule(ctx context.Context, cfs *program.CompiledFileSet) (program.SlotAllocation, error) {
	low, count, err := loadedSlotExtent(cfs)
	if err != nil {
		return program.SlotAllocation{}, err
	}
	used := usedInterfaceNames(cfs)
	bases, skipped, err := func() (program.SlotAllocation, []string, error) {
		registry := &s.loadedGlobals
		registry.mu.Lock()
		defer registry.mu.Unlock()
		identities, skipped, err := s.checkNamedInterfacesLocked(cfs.NamedInterfaces(), used)
		if err != nil {
			return program.SlotAllocation{}, nil, err
		}
		reservation, err := s.reserveLocked(low, count)
		if err != nil {
			return program.SlotAllocation{}, nil, err
		}
		s.registerNamedInterfacesLocked(identities)
		return reservation.bases, skipped, nil
	}()
	if err != nil {
		return program.SlotAllocation{}, err
	}
	reportSkippedInterfaces(ctx, skipped)
	return bases, nil
}

// loadedSlotExtent works out the slots a loaded file set needs.
//
// Takes cfs (*CompiledFileSet) which is the loaded file set.
//
// Returns low (SlotAllocation) which is the lowest slot named per kind.
// Returns count (SlotAllocation) which is the number of slots to reserve per kind.
// Returns error when the operands cannot be read or fall outside the declaration.
func loadedSlotExtent(cfs *program.CompiledFileSet) (low, count program.SlotAllocation, err error) {
	low, count, err = program.GlobalOperandExtent(cfs.Root(), cfs.VariableInitFunction())
	if err != nil {
		return program.SlotAllocation{}, program.SlotAllocation{}, err
	}
	declared := cfs.SlotAllocation()
	if declared == (program.SlotAllocation{}) {
		return low, count, nil
	}
	for kind := range program.NumGlobalRegisterKinds {
		if count[kind] > 0 && int(low[kind])+int(count[kind]) > int(declared[kind]) {
			return program.SlotAllocation{}, program.SlotAllocation{}, fmt.Errorf("%w: the file declares %d %s slots but its code names slot %d",
				fault.ErrBytecodeVerification, declared[kind], isa.RegisterKind(kind), int(low[kind])+int(count[kind])-1)
		}
	}
	return program.SlotAllocation{}, declared, nil
}

// reusableLocked returns the reservation an earlier load of the same bytecode holds, when
// its claim matches.
//
// Takes digest (*[sha256.Size]byte) which is the bytecode's digest, or nil.
// Takes low (SlotAllocation) which is the lowest slot named per kind.
// Takes count (SlotAllocation) which is the slots needed per kind.
//
// Returns *globalReservation which is the reservation to share, or nil.
//
// The caller must hold registry.mu.
func (registry *loadedGlobalRegistry) reusableLocked(digest *[sha256.Size]byte, low, count program.SlotAllocation) *globalReservation {
	if digest == nil {
		return nil
	}
	reservation := registry.byDigest[*digest]
	if reservation == nil || reservation.low != low || reservation.count != count {
		return nil
	}
	return reservation
}

// reserveLocked reserves count slots per kind, taking freed regions first and growing the
// banks otherwise.
//
// Takes low (SlotAllocation) which is the lowest slot named per kind.
// Takes count (SlotAllocation) which is the slots needed per kind.
//
// Returns *globalReservation which holds the regions and bases.
// Returns error which wraps fault.ErrGlobalSlotsExhausted when a bank would overflow.
//
// The caller must hold loadedGlobals.mu, which serialises every load's check and
// reservation.
func (s *Service) reserveLocked(low, count program.SlotAllocation) (*globalReservation, error) {
	reservation := &globalReservation{
		regions:   [program.NumGlobalRegisterKinds]globalSlotRegion{},
		bases:     program.SlotAllocation{},
		low:       low,
		count:     count,
		digest:    [sha256.Size]byte{},
		hasDigest: false,
		handles:   0,
		runs:      0,
	}
	if count == (program.SlotAllocation{}) {
		return reservation, nil
	}
	fromFree, request, err := s.planReservationLocked(reservation)
	if err != nil {
		return nil, err
	}
	if err := s.growForReservation(reservation, request); err != nil {
		return nil, err
	}
	for kind := range program.NumGlobalRegisterKinds {
		if count[kind] == 0 {
			continue
		}
		region := reservation.regions[kind]
		usable := region.start + max(0, int(low[kind])-region.start)
		if fromFree[kind] {
			usable = region.start
		}
		reservation.bases[kind] = safeconv.IntToUint16(usable - int(low[kind]))
	}
	return reservation, nil
}

// planReservationLocked takes a freed run for each kind that has one, and works out how
// far each other bank must grow, padding a bank shorter than the kind's lowest operand.
// When a bank would pass the limit, the runs already taken are given back.
//
// Takes reservation (*globalReservation) which receives the runs taken. Its low and count
// are the claim.
//
// Returns [NumGlobalRegisterKinds]bool which marks the kinds served from freed runs.
// Returns program.SlotAllocation which is the growth each other bank needs.
// Returns error which wraps fault.ErrGlobalSlotsExhausted when a bank would overflow.
//
// The caller must hold loadedGlobals.mu.
func (s *Service) planReservationLocked(reservation *globalReservation) ([program.NumGlobalRegisterKinds]bool, program.SlotAllocation, error) {
	var fromFree [program.NumGlobalRegisterKinds]bool
	var request program.SlotAllocation
	registry := &s.loadedGlobals
	lengths, err := storeLengths(s.globals)
	if err != nil {
		return fromFree, request, err
	}
	low, count := reservation.low, reservation.count
	for kind := range program.NumGlobalRegisterKinds {
		if count[kind] == 0 {
			continue
		}
		if region, ok := registry.takeFreeLocked(kind, int(low[kind]), int(count[kind])); ok {
			reservation.regions[kind] = region
			fromFree[kind] = true
			continue
		}
		padding := low[kind] - min(lengths[kind], low[kind])
		if int(lengths[kind])+int(padding)+int(count[kind]) > maxBankSlots {
			registry.returnTakenLocked(reservation, fromFree)
			return fromFree, request, fmt.Errorf("%w: %d %s slots", fault.ErrGlobalSlotsExhausted, count[kind], isa.RegisterKind(kind))
		}
		request[kind] = padding + count[kind]
	}
	return fromFree, request, nil
}

// growForReservation grows the banks by request and records the new runs on the
// reservation.
//
// Takes reservation (*globalReservation) which receives the runs.
// Takes request (SlotAllocation) which is the growth per kind.
//
// Returns error wrapping fault.ErrGlobalSlotsExhausted when a bank passed the limit.
func (s *Service) growForReservation(reservation *globalReservation, request program.SlotAllocation) error {
	if request == (program.SlotAllocation{}) {
		return nil
	}
	grown, err := s.globals.TryReserveSlots(request)
	if err != nil {
		return fmt.Errorf("the bank grew while the slots were reserved: %w", err)
	}
	for kind := range program.NumGlobalRegisterKinds {
		if request[kind] == 0 {
			continue
		}
		reservation.regions[kind] = globalSlotRegion{start: int(grown[kind]), count: int(request[kind])}
	}
	return nil
}

// takeFreeLocked carves a run of count slots starting at or above low out of a freed
// region of one kind, returning the rest to the free list.
//
// Takes kind (int) which is the register kind.
// Takes low (int) which is the lowest acceptable start.
// Takes count (int) which is the run's length.
//
// Returns globalSlotRegion which is the run taken.
// Returns bool which is false when no freed region fits.
//
// The caller must hold registry.mu.
func (registry *loadedGlobalRegistry) takeFreeLocked(kind, low, count int) (globalSlotRegion, bool) {
	regions := registry.free[kind]
	for index, region := range regions {
		start := max(region.start, low)
		end := region.start + region.count
		if start+count > end {
			continue
		}
		regions = slices.Delete(regions, index, index+1)
		if start > region.start {
			regions = append(regions, globalSlotRegion{start: region.start, count: start - region.start})
		}
		if start+count < end {
			regions = append(regions, globalSlotRegion{start: start + count, count: end - start - count})
		}
		registry.free[kind] = sortedRegions(regions)
		return globalSlotRegion{start: start, count: count}, true
	}
	return globalSlotRegion{}, false
}

// returnTakenLocked gives back the freed regions a failed reservation had taken.
//
// Takes reservation (*globalReservation) which holds the regions.
// Takes fromFree ([NumGlobalRegisterKinds]bool) which marks the kinds taken from the free
// list.
//
// The caller must hold registry.mu.
func (registry *loadedGlobalRegistry) returnTakenLocked(reservation *globalReservation, fromFree [program.NumGlobalRegisterKinds]bool) {
	for kind := range program.NumGlobalRegisterKinds {
		if fromFree[kind] {
			registry.free[kind] = sortedRegions(append(registry.free[kind], reservation.regions[kind]))
		}
	}
}

// releaseIfIdleLocked frees a reservation no load holds and no run uses.
//
// Takes reservation (*globalReservation) which may now be idle.
//
// The caller must hold registry.mu.
func (s *Service) releaseIfIdleLocked(reservation *globalReservation) {
	if reservation.handles > 0 || reservation.runs > 0 {
		return
	}
	registry := &s.loadedGlobals
	if reservation.hasDigest && registry.byDigest[reservation.digest] == reservation {
		delete(registry.byDigest, reservation.digest)
	}
	for kind := range program.NumGlobalRegisterKinds {
		region := reservation.regions[kind]
		if region.count == 0 || s.globals.ZeroSlots(isa.RegisterKind(kind), region.start, region.count) != nil {
			continue
		}
		registry.free[kind] = sortedRegions(append(registry.free[kind], region))
	}
	reservation.regions = [program.NumGlobalRegisterKinds]globalSlotRegion{}
}

// sortedRegions orders regions by start and merges neighbours, so freed runs coalesce.
//
// Takes regions ([]globalSlotRegion) which are the free runs of one kind.
//
// Returns []globalSlotRegion which is sorted and merged.
func sortedRegions(regions []globalSlotRegion) []globalSlotRegion {
	slices.SortFunc(regions, func(a, b globalSlotRegion) int { return a.start - b.start })
	merged := regions[:0]
	for _, region := range regions {
		if last := len(merged) - 1; last >= 0 && merged[last].start+merged[last].count == region.start {
			merged[last].count += region.count
			continue
		}
		merged = append(merged, region)
	}
	return merged
}

// checkNamedInterfacesLocked builds the identities a loaded file set declares and checks
// them against those already registered.
//
// Takes interfaces ([]program.NamedInterface) which are the decoded entries.
// Takes used (map[string]struct{}) which holds the qualified names the program's calls
// name as "*pkg.T".
//
// Returns []*typemodel.Type which are the identities still to register.
// Returns []string which are the conflicting names left as they were.
// Returns error when a used entry conflicts or the interface name limit would be passed.
//
// The caller must hold loadedGlobals.mu.
func (s *Service) checkNamedInterfacesLocked(interfaces []program.NamedInterface, used map[string]struct{}) ([]*typemodel.Type, []string, error) {
	registry := &s.loadedGlobals
	var identities []*typemodel.Type
	var skipped []string
	added := 0
	for _, entry := range interfaces {
		methods := slices.Sorted(slices.Values(entry.Methods))
		identity := typemodel.NewNamedInterfaceType(entry.PackagePath, entry.Name, methods)
		if existing, ok := s.globals.UserNamedInterface(identity.QualifiedName); ok {
			if sameInterfaceMethods(existing, methods) {
				continue
			}
			if _, named := used[identity.QualifiedName]; named {
				return nil, nil, fmt.Errorf("%w: interface %s is already registered with different methods", fault.ErrBytecodeVerification, identity.QualifiedName)
			}
			skipped = append(skipped, identity.QualifiedName)
			continue
		}
		if _, known := registry.interfaceNames[identity.QualifiedName]; !known {
			added++
		}
		identities = append(identities, identity)
	}
	if len(registry.interfaceNames)+added > s.maxLoadedNamedInterfaces() {
		return nil, nil, fmt.Errorf("%w: %d names", ErrNamedInterfaceLimit, s.maxLoadedNamedInterfaces())
	}
	return identities, skipped, nil
}

// sameInterfaceMethods reports whether a registered interface has exactly the given
// sorted method names.
//
// Takes existing (*typemodel.Type) which is the registered identity.
// Takes methods ([]string) which are the sorted method names to compare.
//
// Returns bool which is true when the method sets match.
func sameInterfaceMethods(existing *typemodel.Type, methods []string) bool {
	_, _, existingMethods, ok := existing.NamedInterfaceParts()
	return ok && slices.Equal(slices.Sorted(slices.Values(existingMethods)), methods)
}

// usedInterfaceNames returns the qualified names a file set's calls name as "*pkg.T".
//
// Takes cfs (*CompiledFileSet) which is the loaded file set.
//
// Returns map[string]struct{} which holds the names.
func usedInterfaceNames(cfs *program.CompiledFileSet) map[string]struct{} {
	names := make(map[string]struct{})
	visited := make(map[*program.CompiledFunction]struct{})
	collectPointerStaticTypeNames(cfs.Root(), names, visited)
	collectPointerStaticTypeNames(cfs.VariableInitFunction(), names, visited)
	return names
}

// reportSkippedInterfaces logs, once per load, the declared interfaces left registered
// with another program's method set.
//
// Takes skipped ([]string) which are the qualified names.
func reportSkippedInterfaces(ctx context.Context, skipped []string) {
	if len(skipped) == 0 {
		return
	}
	_, logger := logging.From(ctx)
	logger.Warn("Loaded bytecode declares interfaces already registered with other methods, so the registered ones are kept",
		slog.Any("interfaces", skipped))
}

// registerNamedInterfacesLocked registers identities checkNamedInterfacesLocked passed.
//
// Takes identities ([]*typemodel.Type) which are the identities to register.
//
// The caller must hold loadedGlobals.mu.
func (s *Service) registerNamedInterfacesLocked(identities []*typemodel.Type) {
	registry := &s.loadedGlobals
	for _, identity := range identities {
		s.globals.RegisterUserNamedInterface(identity.QualifiedName, identity)
		if registry.interfaceNames == nil {
			registry.interfaceNames = make(map[string]struct{})
		}
		registry.interfaceNames[identity.QualifiedName] = struct{}{}
	}
}

// maxLoadedNamedInterfaces returns the configured interface name limit.
//
// Returns int which is the limit.
func (s *Service) maxLoadedNamedInterfaces() int {
	if s.config == nil || s.config.maxLoadedNamedInterfaces <= 0 {
		return defaultMaxLoadedNamedInterfaces
	}
	return s.config.maxLoadedNamedInterfaces
}

// trackLoadedProgramLocked records a loaded file set, with a cleanup that drops the
// record once the file set is collected. A collected file set's reservation is kept, so
// function values it handed the host never see their slots reused.
//
// Takes cfs (*CompiledFileSet) which is the loaded file set.
// Takes reservation (*globalReservation) which holds its slots.
//
// The caller must hold loadedGlobals.mu.
func (s *Service) trackLoadedProgramLocked(cfs *program.CompiledFileSet, reservation *globalReservation) {
	registry := &s.loadedGlobals
	if registry.programs == nil {
		registry.programs = make(map[weak.Pointer[program.CompiledFileSet]]*loadedProgram)
	}
	key := weak.Make(cfs)
	registry.programs[key] = &loadedProgram{reservation: reservation, unloaded: false}
	runtime.AddCleanup(cfs, dropLoadedProgram, fileSetRecordOwner{service: weak.Make(s), fileSet: key})
}

// dropLoadedProgram is the cleanup that removes a collected file set's record.
//
// Takes owner (fileSetRecordOwner) which names the service and the file set.
//
// Safe for concurrent use. Runs on the runtime's cleanup goroutine and locks
// loadedGlobals.mu.
func dropLoadedProgram(owner fileSetRecordOwner) {
	service := owner.service.Value()
	if service == nil {
		return
	}
	registry := &service.loadedGlobals
	registry.mu.Lock()
	defer registry.mu.Unlock()
	delete(registry.programs, owner.fileSet)
}

// invalidateLoadedPrograms marks every loaded program unloaded and forgets every
// reservation and freed region, for Reset and Close. A loaded program's slots no longer
// exist once the store is reset, so its later runs are refused.
//
// Safe for concurrent use. It locks loadedGlobals.mu.
func (s *Service) invalidateLoadedPrograms() {
	registry := &s.loadedGlobals
	registry.mu.Lock()
	defer registry.mu.Unlock()
	for _, record := range registry.programs {
		record.unloaded = true
	}
	registry.byDigest = nil
	registry.free = [program.NumGlobalRegisterKinds][]globalSlotRegion{}
}

// forgetFileSetRecords drops the variable-initialiser reuse state and ExecuteInits record
// of an unloaded file set, so nothing watches the slots it gave back.
//
// Takes cfs (*CompiledFileSet) which was unloaded.
func (s *Service) forgetFileSetRecords(cfs *program.CompiledFileSet) {
	owner := fileSetRecordOwner{service: weak.Make(s), fileSet: weak.Make(cfs)}
	dropVarInitReuseState(owner)
	dropInitsRun(owner)
}

// storeLengths reads the store's bank lengths, reporting a store left past the slot
// limit, which the store itself cannot describe, as exhausted rather than panicking.
//
// Takes globals (*engine.GlobalStore) which is the store.
//
// Returns lengths (SlotAllocation) which is the per-kind bank lengths.
// Returns err (error) which wraps fault.ErrGlobalSlotsExhausted when a bank is past the
// limit.
func storeLengths(globals *engine.GlobalStore) (lengths program.SlotAllocation, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			lengths = program.SlotAllocation{}
			err = fmt.Errorf("%w: a bank holds more than %d slots", fault.ErrGlobalSlotsExhausted, maxBankSlots)
		}
	}()
	return globals.Lengths(), nil
}

// beginSlotRollback notes the store's bank lengths before a compile, so a failed compile
// can give back the slots it reserved and leave the service usable.
//
// Returns func(error) which, given a non-nil error, shortens the banks to the noted
// lengths and drops freed regions past them. It does nothing for a nil error.
//
// Concurrent access is guarded by a mutex. The registry mutex guards changes to freed
// regions during rollback.
func (s *Service) beginSlotRollback() func(error) {
	start, err := storeLengths(s.globals)
	if err != nil {
		return func(error) {}
	}
	return func(failure error) {
		if failure == nil {
			return
		}
		registry := &s.loadedGlobals
		registry.mu.Lock()
		defer registry.mu.Unlock()
		s.globals.TruncateSlots(start)
		for kind := range program.NumGlobalRegisterKinds {
			registry.free[kind] = slices.DeleteFunc(registry.free[kind], func(region globalSlotRegion) bool {
				return region.start+region.count > int(start[kind])
			})
		}
	}
}
