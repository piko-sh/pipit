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

//go:build integration && pipit_reuse_audit

package bytecode_test

import (
	"fmt"
	"os"
	"sync/atomic"

	"pipit.sh/pipit/internal/engine"
)

var auditExcusedHits atomic.Int64

func auditExcuse(hits int) {
	auditExcusedHits.Add(int64(hits))
}

func auditFinalReport(code int) int {
	hits := engine.AuditReuse(engine.AuditOptions{MaxPasses: 0, KeepRecords: false, IncludeActiveRuns: true})
	stats := engine.AuditStats()
	excused := auditExcusedHits.Load()
	unexplained := int64(stats.ReleaseHits+stats.OutsideReachHits) - excused
	_, _ = fmt.Fprintf(os.Stderr, "pipit-audit: final release audit: %d hits; recorded=%d skippedSmall=%d skippedEmpty=%d releaseAudits=%d midRunChecks=%d releaseHits=%d inRunUnrooted=%d outsideReach=%d inRunOnly=%d excused=%d\n",
		len(hits), stats.Recorded, stats.SkippedSmall, stats.SkippedEmpty, stats.ReleaseAudits, stats.MidRunChecks,
		stats.ReleaseHits, stats.InRunUnrootedHits, stats.OutsideReachHits, stats.InRunOnlyHits, excused)
	for _, hit := range hits {
		_, _ = fmt.Fprintf(os.Stderr, "pipit-audit: final: %s\n", hit)
	}
	if code == 0 && unexplained > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "pipit-audit: FAIL: %d release and outside-reach hits\n", unexplained)
		code = 1
	}
	return code
}
