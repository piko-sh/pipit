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

//go:build !safe && !(js && wasm)

package engine

import (
	"sync/atomic"
)

const (
	// slabPageShift is log2 of the page size the table counts.
	slabPageShift = 13

	// slabRegionShift is log2 of the address span of one second-level array.
	slabRegionShift = 32

	// slabAddressBits is the address width the table covers.
	slabAddressBits = 48

	// slabPagesPerRegion is the number of counters in one second-level array.
	slabPagesPerRegion = 1 << (slabRegionShift - slabPageShift)
)

// slabPageRegion counts the registered slabs touching each page of one region.
type slabPageRegion [slabPagesPerRegion]atomic.Int32

// slabPageRegions is the first level of the table, indexed by address >> slabRegionShift.
var slabPageRegions [1 << (slabAddressBits - slabRegionShift)]atomic.Pointer[slabPageRegion]

// slabPagesAdd adds delta to every page count in [start, end) and supports concurrent
// calls.
//
// Takes start (uintptr) which is the range's first address.
// Takes end (uintptr) which is one past its last address.
// Takes delta (int32) which is +1 when the range is registered and -1 when dropped.
func slabPagesAdd(start, end uintptr, delta int32) {
	if end <= start || (end-1)>>slabAddressBits != 0 {
		return
	}
	for page := start >> slabPageShift; page <= (end-1)>>slabPageShift; page++ {
		slabPageRegionOf(page).counter(page).Add(delta)
	}
}

// slabPageRegionOf returns the region holding page, making it on first use.
//
// Takes page (uintptr) which is a page number below 1 << (slabAddressBits -
// slabPageShift).
//
// Returns *slabPageRegion which is never nil.
func slabPageRegionOf(page uintptr) *slabPageRegion {
	slot := &slabPageRegions[page>>(slabRegionShift-slabPageShift)]
	if region := slot.Load(); region != nil {
		return region
	}
	fresh := new(slabPageRegion)
	if slot.CompareAndSwap(nil, fresh) {
		return fresh
	}
	return slot.Load()
}

// counter returns the counter of page within the region.
//
// Takes page (uintptr) which is a page number inside the region.
//
// Returns *atomic.Int32 which is the counter.
func (r *slabPageRegion) counter(page uintptr) *atomic.Int32 {
	return &r[page&(slabPagesPerRegion-1)]
}

// slabPagesMayHold reports whether some registered slab may hold address.
//
// Takes address (uintptr) which is the address.
//
// Returns bool which is false only when no registered slab touches address's page.
func slabPagesMayHold(address uintptr) bool {
	if address>>slabAddressBits != 0 {
		return true
	}
	page := address >> slabPageShift
	region := slabPageRegions[page>>(slabRegionShift-slabPageShift)].Load()
	return region != nil && region.counter(page).Load() != 0
}
