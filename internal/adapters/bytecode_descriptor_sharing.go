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

package adapters

import (
	"reflect"
	"strconv"
	"strings"
	"sync"

	flatbuffers "github.com/google/flatbuffers/go"

	"pipit.sh/pipit/internal/codec"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/internal/symtab/descriptor"
)

// descriptorShareMemos holds one packing memo per in-flight builder, so each identical
// descriptor subtree is written once and its offset reused by every parent.
var descriptorShareMemos sync.Map

// descriptorShareMemo is one build's map from a subtree's structural key to the offset
// the subtree was written at.
type descriptorShareMemo struct {
	// offsets maps a structural key to the already-written table or vector offset.
	offsets map[string]flatbuffers.UOffsetT
}

// lookup returns the offset a structural key was already written at.
//
// Takes key (string) which is the subtree's structural key.
//
// Returns the offset and true, or 0 and false when the subtree is new or sharing is off.
func (m *descriptorShareMemo) lookup(key string) (flatbuffers.UOffsetT, bool) {
	if m == nil {
		return 0, false
	}
	offset, ok := m.offsets[key]
	return offset, ok
}

// remember records the offset a structural key was written at.
//
// Takes key (string) which is the subtree's structural key.
// Takes offset (flatbuffers.UOffsetT) which is where the subtree was written.
func (m *descriptorShareMemo) remember(key string, offset flatbuffers.UOffsetT) {
	if m == nil {
		return
	}
	m.offsets[key] = offset
}

// descriptorDecodeCache holds one load's decoded type descriptors, keyed by table offset,
// so a subtree the packer shared is decoded once rather than once per reference.
type descriptorDecodeCache struct {
	// data maps a TypeDescriptor table's offset to the decoded descriptor shared by every
	// reference to it, so the expansion budget charges each table once.
	data map[flatbuffers.UOffsetT]*descriptor.TypeDescriptorData

	// reflects maps a TypeDescriptor table's offset to the reflect.Type reconstructed from
	// it, so a type table that names the same table many times resolves it once.
	reflects map[flatbuffers.UOffsetT]reflect.Type
}

// newDescriptorDecodeCache builds an empty cache for one load.
//
// Returns *descriptorDecodeCache which is scoped to a single payload decode.
func newDescriptorDecodeCache() *descriptorDecodeCache {
	return &descriptorDecodeCache{
		data:     make(map[flatbuffers.UOffsetT]*descriptor.TypeDescriptorData),
		reflects: make(map[flatbuffers.UOffsetT]reflect.Type),
	}
}

// lookupDecoded returns the descriptor already decoded for a table offset.
//
// Takes offset (flatbuffers.UOffsetT) which is the table's position in the payload.
//
// Returns the shared descriptor and true, or nil and false when it has not been decoded.
func (c *descriptorDecodeCache) lookupDecoded(offset flatbuffers.UOffsetT) (*descriptor.TypeDescriptorData, bool) {
	if c == nil {
		return nil, false
	}
	decoded, ok := c.data[offset]
	return decoded, ok
}

// rememberDecoded records the descriptor decoded for a table offset.
//
// Takes offset (flatbuffers.UOffsetT) which is the table's position in the payload.
// Takes decoded (*descriptor.TypeDescriptorData) which every later reference shares.
func (c *descriptorDecodeCache) rememberDecoded(offset flatbuffers.UOffsetT, decoded *descriptor.TypeDescriptorData) {
	if c == nil {
		return
	}
	c.data[offset] = decoded
}

// beginDescriptorSharing starts a sharing scope for one packing run.
//
// Takes builder (*flatbuffers.Builder) which is the builder the run writes into.
//
// Returns func() which ends the scope and must be called when the run finishes.
func beginDescriptorSharing(builder *flatbuffers.Builder) func() {
	descriptorShareMemos.Store(builder, &descriptorShareMemo{offsets: make(map[string]flatbuffers.UOffsetT)})
	return func() { descriptorShareMemos.Delete(builder) }
}

// descriptorMemoFor returns the sharing memo for a builder, or nil when the builder is
// packing outside a sharing scope, in which case every subtree is written out in full.
//
// Takes builder (*flatbuffers.Builder) which is the builder being written into.
//
// Returns *descriptorShareMemo which is the memo, or nil.
func descriptorMemoFor(builder *flatbuffers.Builder) *descriptorShareMemo {
	stored, ok := descriptorShareMemos.Load(builder)
	if !ok {
		return nil
	}
	memo, ok := stored.(*descriptorShareMemo)
	if !ok {
		return nil
	}
	return memo
}

// appendKeyPart appends one variable-length component of a structural key,
// length-prefixed so two different splits can never spell the same key.
//
// Takes out (*strings.Builder) which accumulates the key.
// Takes part (string) which is the component to append.
func appendKeyPart(out *strings.Builder, part string) {
	_, _ = out.WriteString(strconv.Itoa(len(part)))
	_, _ = out.WriteString(":")
	_, _ = out.WriteString(part)
}

// appendKeyInt appends one fixed component of a structural key.
//
// Takes out (*strings.Builder) which accumulates the key.
// Takes value (int64) which is the component to append.
func appendKeyInt(out *strings.Builder, value int64) {
	_, _ = out.WriteString(strconv.FormatInt(value, 10))
	_, _ = out.WriteString(",")
}

// boolKeyValue renders a bool as a structural-key component.
//
// Takes value (bool) which is the flag to render.
//
// Returns int64 which is 1 for true and 0 for false.
func boolKeyValue(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

// reflectTypeForTable reconstructs the reflect.Type for a descriptor table, reusing the
// one already built when the table has been resolved before in this load.
//
// A type table that names the same table many times, or two functions that share a type,
// otherwise pay a full reconstruction per mention even though the result is identical.
//
// Takes cache (*descriptorDecodeCache) which holds this load's resolutions; may be nil.
// Takes offset (flatbuffers.UOffsetT) which is the descriptor table's position.
// Takes data (descriptor.TypeDescriptorData) which is the decoded descriptor.
// Takes registry (*symtab.SymbolRegistry) which resolves named types.
//
// Returns reflect.Type which is the reconstructed type.
// Returns error when the descriptor cannot be reconstructed.
func reflectTypeForTable(cache *descriptorDecodeCache, offset flatbuffers.UOffsetT, data descriptor.TypeDescriptorData, registry *symtab.SymbolRegistry) (reflect.Type, error) {
	if cache != nil {
		if resolved, ok := cache.reflects[offset]; ok {
			return resolved, nil
		}
	}
	resolved, err := codec.DescriptorToReflectType(data, registry)
	if err != nil {
		return nil, err
	}
	if cache != nil {
		cache.reflects[offset] = resolved
	}
	return resolved, nil
}
