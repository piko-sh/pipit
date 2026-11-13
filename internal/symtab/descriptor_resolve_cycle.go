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

package symtab

import (
	"context"
	"fmt"
	"path"
	"reflect"
	"slices"
	"strings"

	"pipit.sh/pipit/internal/symtab/descriptor"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

// descriptorCycle is the group of self-referential named composite types a named
// descriptor belongs to, read from the back-references in its description.
//
// The encoder describes a self-referential type once and writes each recurrence of it, or
// of another type of its group, inside that description as a back-reference to the
// enclosing occurrence (descriptor.TypeDescriptor.BackReference). A back-reference joins
// every named type between it and the occurrence it names into one group. A named
// descriptor whose own description holds a back-reference to its group is built with the
// group (typemodel.NamedCompositeCycleFor), as the compiler builds it.
type descriptorCycle struct {
	// definitions maps each member's qualified key to its first full occurrence.
	definitions map[string]*descriptor.TypeDescriptor

	// reaching memoises reachesMember per node.
	reaching map[*descriptor.TypeDescriptor]bool

	// nodes are every occurrence of a member, full or back-reference.
	nodes []*descriptor.TypeDescriptor
}

// cycleUnion is the union-find descriptorCycleOf builds over named types by key.
type cycleUnion struct {
	// parent maps each key to its parent in the union-find forest.
	parent map[string]string

	// size maps each representative key to the number of keys in its group.
	size map[string]int

	// cyclic marks the representative keys whose groups hold a back-reference.
	cyclic map[string]bool
}

// cycleWalk is the state of descriptorCycleOf's walk over a description.
type cycleWalk struct {
	// union groups the named types the back-references join.
	union *cycleUnion

	// groups caches the group worked out for each representative key, nil for one with no
	// back-reference.
	groups map[string]*descriptorCycle

	// full lists the named occurrences that carry their underlying type.
	full []*descriptor.TypeDescriptor

	// all lists every named occurrence, full or back-reference.
	all []*descriptor.TypeDescriptor

	// pathKeys are the keys of the full named occurrences enclosing the current node,
	// outermost first.
	pathKeys []string
}

// find returns key's representative, pointing every key on the way at it.
//
// Takes key (string) which is a named type's key.
//
// Returns string which is the representative.
func (u *cycleUnion) find(key string) string {
	root := key
	for {
		parent, ok := u.parent[root]
		if !ok || parent == root {
			break
		}
		root = parent
	}
	for key != root {
		next := u.parent[key]
		u.parent[key] = root
		key = next
	}
	return root
}

// join puts a and b in one group, hanging the smaller group under the larger and keeping
// the cyclic mark.
//
// Takes a (string) which is a key.
// Takes b (string) which is another key.
func (u *cycleUnion) join(a, b string) {
	rootA, rootB := u.find(a), u.find(b)
	if rootA == rootB {
		return
	}
	if u.groupSize(rootA) < u.groupSize(rootB) {
		rootA, rootB = rootB, rootA
	}
	u.parent[rootB] = rootA
	u.size[rootA] = u.groupSize(rootA) + u.groupSize(rootB)
	delete(u.size, rootB)
	if u.cyclic[rootB] {
		u.cyclic[rootA] = true
	}
}

// add enters key as a group of its own, unless it is already known.
//
// Takes key (string) which is a named type's key.
func (u *cycleUnion) add(key string) {
	if _, known := u.parent[key]; !known {
		u.parent[key] = key
	}
}

// groupSize returns the number of keys in a representative's group.
//
// Takes root (string) which is a representative key.
//
// Returns int which is at least one.
func (u *cycleUnion) groupSize(root string) int {
	return max(u.size[root], 1)
}

// groupOf returns the group of the named type with the given key, worked out on first
// use, or nil when the group holds no back-reference.
//
// Takes key (string) which is a named type's key.
//
// Returns *descriptorCycle which is the group, or nil.
func (walk *cycleWalk) groupOf(key string) *descriptorCycle {
	rootKey := walk.union.find(key)
	if !walk.union.cyclic[rootKey] {
		return nil
	}
	if cycle, known := walk.groups[rootKey]; known {
		return cycle
	}
	cycle := &descriptorCycle{definitions: make(map[string]*descriptor.TypeDescriptor), reaching: make(map[*descriptor.TypeDescriptor]bool), nodes: nil}
	for _, node := range walk.full {
		nodeKey := descriptorCycleKey(node)
		if _, defined := cycle.definitions[nodeKey]; !defined && walk.union.find(nodeKey) == rootKey {
			cycle.definitions[nodeKey] = node
		}
	}
	for _, node := range walk.all {
		if walk.union.find(descriptorCycleKey(node)) == rootKey {
			cycle.nodes = append(cycle.nodes, node)
		}
	}
	walk.groups[rootKey] = cycle
	return cycle
}

// visit walks node and its children, joining each back-reference with the occurrences it
// passes through.
//
// Takes node (*descriptor.TypeDescriptor) which is the node, possibly nil.
// Takes depth (int) which bounds the walk.
//
// Returns error when the walk is too deep or a back-reference names no enclosing
// occurrence.
func (walk *cycleWalk) visit(node *descriptor.TypeDescriptor, depth int) error {
	if node == nil {
		return nil
	}
	if depth > descriptor.MaxTypeDescriptorDepth {
		return fmt.Errorf("%w: type nesting exceeds %d levels", errCorruptTypeDescriptor, descriptor.MaxTypeDescriptorDepth)
	}
	if node.Kind == descriptor.KindNamed && node.BackReference {
		walk.all = append(walk.all, node)
		return joinBackReference(walk.union, walk.pathKeys, descriptorCycleKey(node))
	}
	if node.Kind == descriptor.KindNamed && node.Element != nil {
		walk.union.add(descriptorCycleKey(node))
		walk.full = append(walk.full, node)
		walk.all = append(walk.all, node)
		walk.pathKeys = append(walk.pathKeys, descriptorCycleKey(node))
		defer func() { walk.pathKeys = walk.pathKeys[:len(walk.pathKeys)-1] }()
	}
	for _, child := range descriptorChildren(node) {
		if err := walk.visit(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// isMember reports whether node is an occurrence of a member.
//
// Takes node (*descriptor.TypeDescriptor) which is the descriptor.
//
// Returns bool which is true for a member's full occurrence or back-reference.
func (cycle *descriptorCycle) isMember(node *descriptor.TypeDescriptor) bool {
	if node.Kind != descriptor.KindNamed {
		return false
	}
	_, member := cycle.definitions[descriptorCycleKey(node)]
	return member
}

// reachesMember reports whether node is, or nests, an occurrence of a member.
//
// Takes node (*descriptor.TypeDescriptor) which is the descriptor.
//
// Returns bool which is true when a member is reached.
func (cycle *descriptorCycle) reachesMember(node *descriptor.TypeDescriptor) bool {
	if known, ok := cycle.reaching[node]; ok {
		return known
	}
	cycle.reaching[node] = false
	found := cycle.isMember(node)
	for _, child := range descriptorChildren(node) {
		if found {
			break
		}
		found = cycle.reachesMember(child)
	}
	cycle.reaching[node] = found
	return found
}

// members describes the group's members for typemodel and maps each member's qualified
// name to its definition.
//
// Takes registry (*SymbolRegistry) which resolves the parts that reach no member.
// Takes depth (int) which is the current nesting depth.
// Takes memo (*reflectTypeMemo) which caches resolved descriptors.
//
// Returns []typemodel.CycleMember which describe the members.
// Returns map[string]*descriptor.TypeDescriptor which maps qualified names to
// definitions.
// Returns error when a member's placeholder or shape cannot be worked out.
func (cycle *descriptorCycle) members(ctx context.Context, registry *SymbolRegistry, depth int, memo *reflectTypeMemo) ([]typemodel.CycleMember, map[string]*descriptor.TypeDescriptor, error) {
	members := make([]typemodel.CycleMember, 0, len(cycle.definitions))
	byName := make(map[string]*descriptor.TypeDescriptor, len(cycle.definitions))
	for _, definition := range cycle.definitions {
		placeholder, err := cycle.placeholder(ctx, definition, registry, depth, memo)
		if err != nil {
			return nil, nil, err
		}
		shape, err := cycle.shape(ctx, definition.Element, registry, depth, memo)
		if err != nil {
			return nil, nil, err
		}
		identity := descriptorIdentity(definition)
		members = append(members, typemodel.CycleMember{Placeholder: placeholder, Identity: identity, Shape: shape})
		byName[identity.Qualified()] = definition
	}
	return members, byName, nil
}

// shape writes a member's underlying descriptor in typemodel's cycle shape notation, as
// the compiler writes the underlying type it was built from.
//
// Takes node (*descriptor.TypeDescriptor) which is the descriptor or a part of it.
// Takes registry (*SymbolRegistry) which resolves the parts that reach no member.
// Takes depth (int) which is the current nesting depth.
// Takes memo (*reflectTypeMemo) which caches resolved descriptors.
//
// Returns string which is the shape.
// Returns error when a part cannot be resolved.
func (cycle *descriptorCycle) shape(ctx context.Context, node *descriptor.TypeDescriptor, registry *SymbolRegistry, depth int, memo *reflectTypeMemo) (string, error) {
	if node == nil {
		return "", fmt.Errorf("%w: missing underlying sub-descriptor", errCorruptTypeDescriptor)
	}
	if cycle.isMember(node) {
		return typemodel.CycleShapeMember(node.PackagePath, node.Name), nil
	}
	if !cycle.reachesMember(node) {
		leaf, err := resolveSubDescriptor(ctx, node, registry, "cycle part", depth, memo)
		if err != nil {
			return "", err
		}
		return typemodel.CycleShapeLeaf(leaf), nil
	}
	children := descriptorChildren(node)
	parts := make([]string, len(children))
	for index, child := range children {
		part, err := cycle.shape(ctx, child, registry, depth+1, memo)
		if err != nil {
			return "", err
		}
		parts[index] = part
	}
	switch node.Kind {
	case descriptor.KindSlice:
		return typemodel.CycleShapeComposite(reflect.Slice, 0, false, parts, 0), nil
	case descriptor.KindArray:
		return typemodel.CycleShapeComposite(reflect.Array, node.Length, false, parts, 0), nil
	case descriptor.KindPtr:
		return typemodel.CycleShapeComposite(reflect.Pointer, 0, false, parts, 0), nil
	case descriptor.KindChan:
		return typemodel.CycleShapeComposite(reflect.Chan, node.Dir, false, parts, 0), nil
	case descriptor.KindMap:
		return typemodel.CycleShapeComposite(reflect.Map, 0, false, parts, 0), nil
	case descriptor.KindFunc:
		return typemodel.CycleShapeComposite(reflect.Func, 0, node.IsVariadic, parts, len(node.Params)), nil
	default:
		return "", fmt.Errorf("%w: a %d descriptor cannot lead to a self-referential type", errCorruptTypeDescriptor, node.Kind)
	}
}

// placeholder returns a member's placeholder for typemodel (typemodel.CycleMember).
//
// Takes definition (*descriptor.TypeDescriptor) which is the member's full occurrence.
// Takes registry (*SymbolRegistry) which resolves array element parts.
// Takes depth (int) which is the current nesting depth.
// Takes memo (*reflectTypeMemo) which caches resolved descriptors.
//
// Returns reflect.Type which is the placeholder.
// Returns error when the member's kind cannot be a member.
func (cycle *descriptorCycle) placeholder(ctx context.Context, definition *descriptor.TypeDescriptor, registry *SymbolRegistry, depth int, memo *reflectTypeMemo) (reflect.Type, error) {
	underlying := definition.Element
	switch underlying.Kind {
	case descriptor.KindSlice:
		return typemodel.CyclePlaceholder(reflect.Slice, 0, 0, false, 0, nil)
	case descriptor.KindMap:
		return typemodel.CyclePlaceholder(reflect.Map, 0, 0, false, 0, nil)
	case descriptor.KindChan:
		return typemodel.CyclePlaceholder(reflect.Chan, 0, 0, false, 0, nil)
	case descriptor.KindFunc:
		return typemodel.CyclePlaceholder(reflect.Func, len(underlying.Params), len(underlying.Results), underlying.IsVariadic, 0, nil)
	case descriptor.KindArray:
		element, err := cycle.layoutType(ctx, underlying.Element, registry, depth, memo)
		if err != nil {
			return nil, err
		}
		if err := checkReconstructedArraySize(underlying.Length, element); err != nil {
			return nil, err
		}
		return typemodel.CyclePlaceholder(reflect.Array, 0, 0, false, underlying.Length, element)
	default:
		return nil, fmt.Errorf("%w: a %d descriptor cannot be self-referential", errCorruptTypeDescriptor, underlying.Kind)
	}
}

// layoutType returns a type with the layout node will have once the group is built, for
// an array member's placeholder.
//
// Takes node (*descriptor.TypeDescriptor) which is an array member's element or a part of
// it.
// Takes registry (*SymbolRegistry) which resolves the parts that reach no member.
// Takes depth (int) which is the current nesting depth.
// Takes memo (*reflectTypeMemo) which caches resolved descriptors.
//
// Returns reflect.Type which has node's layout.
// Returns error when a part cannot be resolved.
func (cycle *descriptorCycle) layoutType(ctx context.Context, node *descriptor.TypeDescriptor, registry *SymbolRegistry, depth int, memo *reflectTypeMemo) (reflect.Type, error) {
	if node == nil {
		return nil, fmt.Errorf("%w: missing array element", errCorruptTypeDescriptor)
	}
	if depth > descriptor.MaxTypeDescriptorDepth {
		return nil, fmt.Errorf("%w: type nesting exceeds %d levels", errCorruptTypeDescriptor, descriptor.MaxTypeDescriptorDepth)
	}
	if cycle.isMember(node) {
		return cycle.placeholder(ctx, cycle.definitions[descriptorCycleKey(node)], registry, depth+1, memo)
	}
	if !cycle.reachesMember(node) {
		return resolveSubDescriptor(ctx, node, registry, "array element", depth, memo)
	}
	switch node.Kind {
	case descriptor.KindArray:
		element, err := cycle.layoutType(ctx, node.Element, registry, depth+1, memo)
		if err != nil {
			return nil, err
		}
		if err := checkReconstructedArraySize(node.Length, element); err != nil {
			return nil, err
		}
		return safeReflectComposite(func() reflect.Type { return reflect.ArrayOf(node.Length, element) })
	case descriptor.KindSlice:
		return typemodel.CyclePlaceholder(reflect.Slice, 0, 0, false, 0, nil)
	case descriptor.KindMap:
		return typemodel.CyclePlaceholder(reflect.Map, 0, 0, false, 0, nil)
	case descriptor.KindChan:
		return typemodel.CyclePlaceholder(reflect.Chan, 0, 0, false, 0, nil)
	case descriptor.KindPtr:
		return reflect.TypeFor[*byte](), nil
	case descriptor.KindFunc:
		return typemodel.CyclePlaceholder(reflect.Func, len(node.Params), len(node.Results), node.IsVariadic, 0, nil)
	default:
		return nil, fmt.Errorf("%w: a %d descriptor cannot lead to a self-referential type", errCorruptTypeDescriptor, node.Kind)
	}
}

// descriptorCycleKey keys a named descriptor by package path and name.
//
// Takes node (*descriptor.TypeDescriptor) which is a KindNamed descriptor.
//
// Returns string which is the key.
func descriptorCycleKey(node *descriptor.TypeDescriptor) string {
	return node.PackagePath + "\x00" + node.Name
}

// newCycleWalk returns a walk with no named types seen yet.
//
// Returns *cycleWalk which is empty.
func newCycleWalk() *cycleWalk {
	return &cycleWalk{
		union:    &cycleUnion{parent: make(map[string]string), size: make(map[string]int), cyclic: make(map[string]bool)},
		groups:   make(map[string]*descriptorCycle),
		full:     nil,
		all:      nil,
		pathKeys: nil,
	}
}

// descriptorCycleOf returns the group node belongs to, or nil when its group holds no
// back-reference.
//
// The groups come from one walk of the reconstruction's top-level descriptor, made on
// first use and kept in memo, so a descriptor nesting many named descriptors is walked
// once rather than once for each of them. Each group is worked out once as well.
//
// Takes node (*descriptor.TypeDescriptor) which is a KindNamed descriptor with Element.
// Takes memo (*reflectTypeMemo) which holds the walk.
//
// Returns *descriptorCycle which is the group, or nil.
// Returns error when a back-reference names no enclosing occurrence or the descriptor
// nests too deeply.
func descriptorCycleOf(node *descriptor.TypeDescriptor, memo *reflectTypeMemo) (*descriptorCycle, error) {
	if memo.cycles == nil {
		walk := newCycleWalk()
		root := memo.root
		if root == nil {
			root = node
		}
		if err := walk.visit(root, 0); err != nil {
			return nil, err
		}
		if _, found := walk.union.parent[descriptorCycleKey(node)]; !found {
			walk = newCycleWalk()
			if err := walk.visit(node, 0); err != nil {
				return nil, err
			}
		}
		memo.cycles = walk
	}
	return memo.cycles.groupOf(descriptorCycleKey(node)), nil
}

// joinBackReference joins a back-reference's target with every named occurrence between
// it and the back-reference, and marks the group cyclic.
//
// Takes union (*cycleUnion) which records the groups.
// Takes pathKeys ([]string) which are the keys of the enclosing named occurrences,
// outermost first.
// Takes target (string) which is the back-reference's key.
//
// Returns error when no enclosing occurrence has the key.
func joinBackReference(union *cycleUnion, pathKeys []string, target string) error {
	for index, key := range slices.Backward(pathKeys) {
		if key != target {
			continue
		}
		for _, key := range pathKeys[index:] {
			union.join(target, key)
		}
		union.cyclic[union.find(target)] = true
		return nil
	}
	return fmt.Errorf("%w: back-reference to %s outside its description", errCorruptTypeDescriptor, strings.ReplaceAll(target, "\x00", "."))
}

// descriptorChildren returns the descriptors nested directly in node.
//
// Takes node (*descriptor.TypeDescriptor) which is the descriptor.
//
// Returns []*descriptor.TypeDescriptor which are its element, key, value, field,
// parameter and result descriptors.
func descriptorChildren(node *descriptor.TypeDescriptor) []*descriptor.TypeDescriptor {
	children := make([]*descriptor.TypeDescriptor, 0, 3+len(node.Fields)+len(node.Params)+len(node.Results))
	for _, child := range []*descriptor.TypeDescriptor{node.Element, node.Key, node.Value} {
		if child != nil {
			children = append(children, child)
		}
	}
	for index := range node.Fields {
		children = append(children, &node.Fields[index].Typ)
	}
	for index := range node.Params {
		children = append(children, &node.Params[index])
	}
	for index := range node.Results {
		children = append(children, &node.Results[index])
	}
	return children
}

// resolveNamedCompositeCycle resolves the members of a group to their runtime types
// through typemodel.NamedCompositeCycleFor, as the compiler built them, and returns
// root's.
//
// Takes root (descriptor.TypeDescriptor) which is the named descriptor being resolved.
// Takes cycle (*descriptorCycle) which is its group.
// Takes registry (*SymbolRegistry) which resolves the other types.
// Takes depth (int) which is the current nesting depth.
// Takes memo (*reflectTypeMemo) which caches resolved descriptors. Every occurrence of a
// member is entered in it.
//
// Returns reflect.Type which is root's runtime type.
// Returns error when a part cannot be resolved or the group cannot be built.
func resolveNamedCompositeCycle(ctx context.Context, root descriptor.TypeDescriptor, cycle *descriptorCycle, registry *SymbolRegistry, depth int, memo *reflectTypeMemo) (reflect.Type, error) {
	members, byName, err := cycle.members(ctx, registry, depth, memo)
	if err != nil {
		return nil, err
	}
	var forgedTypes map[string]reflect.Type
	mark := len(memo.added)
	built, err := registry.NamedCompositeQuota().CycleFor(members, func(forged map[string]reflect.Type) ([]reflect.Type, error) {
		forgedTypes = forged
		for _, node := range cycle.nodes {
			memo.store(node, forged[descriptorIdentity(node).Qualified()])
		}
		return resolveCycleUnderlying(ctx, members, byName, registry, depth, memo)
	})
	if err != nil {
		memo.rollback(mark)
		return nil, fmt.Errorf("named type %s.%s: %w", root.PackagePath, root.Name, err)
	}
	if forgedTypes != nil && !sameCycleTypes(forgedTypes, built) {
		memo.rollback(mark)
	}
	for _, node := range cycle.nodes {
		memo.store(node, built[descriptorIdentity(node).Qualified()])
	}
	return built[descriptorIdentity(&root).Qualified()], nil
}

// resolveCycleUnderlying resolves each member's underlying descriptor, in member order,
// with every occurrence of a member already entered in memo.
//
// Takes members ([]typemodel.CycleMember) which are the members in typemodel's order.
// Takes byName (map[string]*descriptor.TypeDescriptor) which finds each definition.
// Takes registry (*SymbolRegistry) which resolves the other types.
// Takes depth (int) which is the current nesting depth.
// Takes memo (*reflectTypeMemo) which caches resolved descriptors.
//
// Returns []reflect.Type which are the underlying types.
// Returns error when one cannot be resolved.
func resolveCycleUnderlying(
	ctx context.Context,
	members []typemodel.CycleMember,
	byName map[string]*descriptor.TypeDescriptor,
	registry *SymbolRegistry,
	depth int,
	memo *reflectTypeMemo,
) ([]reflect.Type, error) {
	underlying := make([]reflect.Type, len(members))
	for index, member := range members {
		resolved, err := resolveSubDescriptor(ctx, byName[member.Identity.Qualified()].Element, registry, "underlying", depth, memo)
		if err != nil {
			return nil, err
		}
		underlying[index] = resolved
	}
	return underlying, nil
}

// sameCycleTypes reports whether the types recorded for a group are the ones this
// resolution forged. They differ when another compile recorded the group first, and the
// types resolved from the discarded ones must then be dropped from the memo.
//
// Takes forged (map[string]reflect.Type) which are the types this resolution forged.
// Takes built (map[string]reflect.Type) which are the recorded types.
//
// Returns bool which is true when every member's type is the forged one.
func sameCycleTypes(forged, built map[string]reflect.Type) bool {
	for name, forgedType := range forged {
		if built[name] != forgedType {
			return false
		}
	}
	return true
}

// descriptorIdentity names the named composite type a descriptor describes.
//
// Takes node (*descriptor.TypeDescriptor) which is a KindNamed descriptor.
//
// Returns typemodel.NamedCompositeIdentity which is its identity.
func descriptorIdentity(node *descriptor.TypeDescriptor) typemodel.NamedCompositeIdentity {
	bareName, _, _ := strings.Cut(node.Name, "[")
	return typemodel.NamedCompositeIdentity{
		PkgPath:  node.PackagePath,
		PkgName:  path.Base(node.PackagePath),
		Name:     node.Name,
		BareName: bareName,
	}
}
