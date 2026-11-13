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

package engine

import "sync"

// typedCache is a sync.Map whose keys are K and values V, for concurrent caches and
// registries.
type typedCache[K comparable, V any] struct {
	// entries holds the values.
	entries sync.Map
}

// load returns the value stored for key.
//
// Takes key (K) which is the key.
//
// Returns V which is the value, or the zero value when absent.
// Returns bool which is true when a value of type V was stored.
func (c *typedCache[K, V]) load(key K) (V, bool) {
	raw, found := c.entries.Load(key)
	if !found {
		var zero V
		return zero, false
	}
	return typedCacheValue[V](raw)
}

// store sets the value for key.
//
// Takes key (K) which is the key.
// Takes value (V) which is the value.
func (c *typedCache[K, V]) store(key K, value V) {
	c.entries.Store(key, value)
}

// loadOrStore returns the value stored for key, storing value first when there is none.
//
// Takes key (K) which is the key.
// Takes value (V) which is stored when key is absent.
//
// Returns V which is the value now stored.
// Returns bool which is true when the value was already there.
func (c *typedCache[K, V]) loadOrStore(key K, value V) (V, bool) {
	raw, loaded := c.entries.LoadOrStore(key, value)
	if !loaded {
		return value, false
	}
	if existing, ok := typedCacheValue[V](raw); ok {
		return existing, true
	}
	c.entries.Store(key, value)
	return value, false
}

// loadAndDelete removes key and returns its value.
//
// Takes key (K) which is the key.
//
// Returns V which is the removed value, or the zero value.
// Returns bool which is true when a value of type V was removed.
func (c *typedCache[K, V]) loadAndDelete(key K) (V, bool) {
	raw, loaded := c.entries.LoadAndDelete(key)
	if !loaded {
		var zero V
		return zero, false
	}
	return typedCacheValue[V](raw)
}

// swap stores value for key and returns the value it replaced.
//
// Takes key (K) which is the key.
// Takes value (V) which is the new value.
//
// Returns V which is the replaced value, or the zero value.
// Returns bool which is true when key held a value before (of any type).
func (c *typedCache[K, V]) swap(key K, value V) (V, bool) {
	raw, loaded := c.entries.Swap(key, value)
	if !loaded {
		var zero V
		return zero, false
	}
	previous, _ := typedCacheValue[V](raw)
	return previous, true
}

// compareAndDelete removes key when its value is old.
//
// Takes key (K) which is the key.
// Takes old (V) which is the value it must hold; V must be comparable.
//
// Returns bool which is true when it was removed.
func (c *typedCache[K, V]) compareAndDelete(key K, old V) bool {
	return c.entries.CompareAndDelete(key, old)
}

// delete removes key.
//
// Takes key (K) which is the key.
func (c *typedCache[K, V]) delete(key K) {
	c.entries.Delete(key)
}

// clear removes every entry.
func (c *typedCache[K, V]) clear() {
	c.entries.Clear()
}

// typedCacheValue asserts a stored value's type.
//
// Takes raw (any) which is the stored value.
//
// Returns V which is the value, or the zero value when raw is not a V.
// Returns bool which is true when raw is a V.
func typedCacheValue[V any](raw any) (V, bool) {
	value, ok := raw.(V)
	return value, ok
}
