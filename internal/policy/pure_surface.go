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

package policy

import "slices"

// pureSurface is the default import allowlist for the restricted and isolated tiers: the
// standard packages that compute over their inputs without reaching the host's files,
// network, processes or environment. Side-effecting functions inside these packages stay
// subject to the capability hook and the tier's limits.
var pureSurface = []string{
	"bufio", "bytes", "cmp",
	"container/heap", "container/list", "container/ring",
	"crypto/hmac", "crypto/md5", "crypto/rand", "crypto/sha1",
	"crypto/sha256", "crypto/sha512", "crypto/subtle",
	"encoding", "encoding/ascii85", "encoding/asn1", "encoding/base32",
	"encoding/base64", "encoding/binary", "encoding/csv", "encoding/gob",
	"encoding/hex", "encoding/json", "encoding/pem", "encoding/xml",
	"errors", "fmt",
	"hash", "hash/adler32", "hash/crc32", "hash/crc64", "hash/fnv", "hash/maphash",
	"html", "io", "iter",
	"maps", "math", "math/big", "math/bits", "math/cmplx", "math/rand", "math/rand/v2",
	"path", "regexp", "regexp/syntax",
	"slices", "sort", "strconv", "strings",
	"text/tabwriter", "text/template", "text/template/parse",
	"time",
	"unicode", "unicode/utf16", "unicode/utf8",
}

// PureSurface returns the default import allowlist for the restricted and isolated tiers:
// the standard packages that compute over their inputs without touching host resources.
//
// Returns []string which is a fresh copy the caller may retain and mutate.
func PureSurface() []string {
	return slices.Clone(pureSurface)
}
