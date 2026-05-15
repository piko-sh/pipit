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

package sandboxwire

const (
	// defaultFrameBytes is the frame size limit when zero is requested.
	defaultFrameBytes = 1 << 20

	// maximumFrameBytes is the absolute ceiling for frame size.
	maximumFrameBytes = 8 << 20

	// defaultJSONDepth is the nesting limit when zero is requested.
	defaultJSONDepth = 32

	// maximumJSONDepth is the absolute ceiling for JSON nesting.
	maximumJSONDepth = 64

	// defaultJSONValues is the token limit when zero is requested.
	defaultJSONValues = 65536

	// maximumJSONValues is the absolute ceiling for JSON tokens.
	maximumJSONValues = 65536
)

// Limits bounds allocation and JSON traversal before payload-specific decoding. Zero
// selects finite defaults; negative or excessive values are rejected.
type Limits struct {
	// MaxFrameBytes bounds the encoded envelope, excluding the four-byte header.
	MaxFrameBytes int

	// MaxJSONDepth bounds nested containers, including the envelope itself.
	MaxJSONDepth int

	// MaxJSONValues bounds JSON tokens, including object keys and delimiters.
	MaxJSONValues int
}

// normalise validates limits and substitutes finite defaults.
//
// Returns Limits which contains only positive, bounded values.
// Returns error when any requested value is negative or exceeds its ceiling.
func (limits Limits) normalise() (Limits, error) {
	settings := []struct {
		value    *int
		fallback int
		ceiling  int
	}{
		{value: &limits.MaxFrameBytes, fallback: defaultFrameBytes, ceiling: maximumFrameBytes},
		{value: &limits.MaxJSONDepth, fallback: defaultJSONDepth, ceiling: maximumJSONDepth},
		{value: &limits.MaxJSONValues, fallback: defaultJSONValues, ceiling: maximumJSONValues},
	}
	for _, setting := range settings {
		if *setting.value < 0 || *setting.value > setting.ceiling {
			return Limits{}, ErrInvalidLimits
		}
		if *setting.value == 0 {
			*setting.value = setting.fallback
		}
	}
	return limits, nil
}
