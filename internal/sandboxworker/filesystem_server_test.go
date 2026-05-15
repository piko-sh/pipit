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

package sandboxworker

import (
	"encoding/json"
	"testing"

	"pipit.sh/pipit/internal/sandboxwire"
)

func TestFilesystemWorkerRejectsInvalidConfiguration(t *testing.T) {
	for _, payload := range []string{
		`{"profile":"restricted-filesystem-v1","imports":["pipit/fs"],"roots":[]}`,
		`{"profile":"restricted-filesystem-v1","imports":["pipit/fs"],"roots":null,"limits":{}}`,
		`{"profile":"restricted-filesystem-v1","imports":["pipit/fs"],"roots":[],"limits":null}`,
		`{"profile":"restricted-filesystem-v1","imports":["os","pipit/fs"],"roots":[],"limits":{}}`,
		`{"profile":"restricted-filesystem-v1","imports":[],"roots":[],"limits":{}}`,
		`{"profile":"restricted-filesystem-v1","imports":["pipit/fs"],"roots":[{"Name":"data","Rights":1,"Path":"/tmp"}],"limits":{}}`,
		`{"profile":"restricted-filesystem-v1","imports":["pipit/fs"],"roots":[],"limits":{},"fallback":true}`,
		`{"Profile":"restricted-filesystem-v1","imports":["pipit/fs"],"roots":[],"limits":{}}`,
	} {
		stream := sourceFixture(t, sandboxwire.Message{Kind: sandboxwire.Configure, ID: 0, Payload: json.RawMessage(payload)})
		if err := Serve(stream); err == nil {
			t.Fatalf("accepted invalid config: %s", payload)
		}
	}
}
