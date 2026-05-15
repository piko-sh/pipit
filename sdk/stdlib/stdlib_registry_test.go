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

package stdlib

import (
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var safeStandardLibraryPackages = []string{
	"archive/tar",
	"archive/zip",
	"bufio",
	"bytes",
	"cmp",
	"compress/bzip2",
	"compress/flate",
	"compress/gzip",
	"compress/lzw",
	"compress/zlib",
	"container/heap",
	"container/list",
	"container/ring",
	"context",
	"crypto",
	"crypto/aes",
	"crypto/cipher",
	"crypto/des",
	"crypto/dsa",
	"crypto/ecdh",
	"crypto/ecdsa",
	"crypto/ed25519",
	"crypto/elliptic",
	"crypto/fips140",
	"crypto/hkdf",
	"crypto/hmac",
	"crypto/hpke",
	"crypto/md5",
	"crypto/mldsa",
	"crypto/mlkem",
	"crypto/mlkem/mlkemtest",
	"crypto/pbkdf2",
	"crypto/rand",
	"crypto/rc4",
	"crypto/rsa",
	"crypto/sha1",
	"crypto/sha256",
	"crypto/sha3",
	"crypto/sha512",
	"crypto/subtle",
	"crypto/tls",
	"crypto/x509",
	"crypto/x509/pkix",
	"database/sql",
	"database/sql/driver",
	"debug/buildinfo",
	"debug/dwarf",
	"debug/elf",
	"debug/gosym",
	"debug/macho",
	"debug/pe",
	"debug/plan9obj",
	"embed",
	"encoding",
	"encoding/ascii85",
	"encoding/asn1",
	"encoding/base32",
	"encoding/base64",
	"encoding/binary",
	"encoding/csv",
	"encoding/gob",
	"encoding/hex",
	"encoding/json",
	"encoding/json/jsontext",
	"encoding/json/v2",
	"encoding/pem",
	"encoding/xml",
	"errors",
	"expvar",
	"flag",
	"fmt",
	"go/ast",
	"go/build",
	"go/build/constraint",
	"go/constant",
	"go/doc",
	"go/doc/comment",
	"go/format",
	"go/importer",
	"go/parser",
	"go/printer",
	"go/scanner",
	"go/token",
	"go/types",
	"go/version",
	"hash",
	"hash/adler32",
	"hash/crc32",
	"hash/crc64",
	"hash/fnv",
	"hash/maphash",
	"html",
	"html/template",
	"image",
	"image/color",
	"image/color/palette",
	"image/draw",
	"image/gif",
	"image/jpeg",
	"image/png",
	"index/suffixarray",
	"io",
	"io/fs",
	"io/ioutil",
	"iter",
	"log",
	"log/slog",
	"log/syslog",
	"maps",
	"math",
	"math/big",
	"math/bits",
	"math/cmplx",
	"math/rand",
	"math/rand/v2",
	"mime",
	"mime/multipart",
	"mime/quotedprintable",
	"net",
	"net/http",
	"net/http/cgi",
	"net/http/cookiejar",
	"net/http/fcgi",
	"net/http/httptest",
	"net/http/httptrace",
	"net/http/httputil",
	"net/http/pprof",
	"net/mail",
	"net/netip",
	"net/rpc",
	"net/rpc/jsonrpc",
	"net/smtp",
	"net/textproto",
	"net/url",
	"os",
	"os/exec",
	"os/signal",
	"os/user",
	"path",
	"path/filepath",
	"reflect",
	"regexp",
	"regexp/syntax",
	"runtime",
	"runtime/debug",
	"runtime/metrics",
	"runtime/pprof",
	"slices",
	"sort",
	"strconv",
	"strings",
	"structs",
	"sync",
	"sync/atomic",
	"testing",
	"testing/cryptotest",
	"testing/fstest",
	"testing/iotest",
	"testing/quick",
	"testing/slogtest",
	"testing/synctest",
	"text/scanner",
	"text/tabwriter",
	"text/template",
	"text/template/parse",
	"time",
	"time/tzdata",
	"unicode",
	"unicode/utf16",
	"unicode/utf8",
	"unique",
	"uuid",
	"weak",
}

var hostRestrictedPackages = map[string][]string{
	"log/syslog": {"windows", "plan9"},
}

func packageAvailableOnHost(importPath string) bool {
	return !slices.Contains(hostRestrictedPackages[importPath], runtime.GOOS)
}

func TestRegistryCarriesEverySafeStandardLibraryPackage(t *testing.T) {
	t.Parallel()

	exports := Exports()

	for _, importPath := range safeStandardLibraryPackages {
		if !packageAvailableOnHost(importPath) {
			continue
		}
		_, ok := exports[importPath]
		require.True(t, ok, "standard library package %q is not registered", importPath)
	}
}

func TestRegistryStandardLibraryListIsCurrent(t *testing.T) {
	t.Parallel()

	expected := make(map[string]struct{}, len(safeStandardLibraryPackages))
	for _, importPath := range safeStandardLibraryPackages {
		expected[importPath] = struct{}{}
	}

	for importPath := range Exports() {
		first, _, _ := strings.Cut(importPath, "/")
		if strings.Contains(first, ".") {
			continue
		}
		_, ok := expected[importPath]
		require.True(t, ok, "registered standard library package %q is missing from safeStandardLibraryPackages", importPath)
	}
}
