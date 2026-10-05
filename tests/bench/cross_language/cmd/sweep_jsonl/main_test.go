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

package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decodeReport(t *testing.T, text string) report {
	t.Helper()
	var decoded report
	require.NoError(t, json.Unmarshal([]byte(text), &decoded))
	return decoded
}

func TestConvertLaterReportReplacesItsRunners(t *testing.T) {
	sweep := decodeReport(t, `{"generated_at_utc":"t1","host":{"os":"linux","arch":"amd64","cpu_count_logical":8},
		"runs":[{"benchmark":"b","runner":"cpython","mode":"innerloop","status":"ok"},
		        {"benchmark":"b","runner":"pipit","mode":"innerloop","status":"ok"},
		        {"benchmark":"c","runner":"pipit","mode":"innerloop","status":"ok"}],
		"aggregates":[{"benchmark":"b","runner":"cpython","mode":"innerloop","runs":3,"median_nanos":30},
		              {"benchmark":"b","runner":"pipit","mode":"innerloop","runs":3,"median_nanos":20},
		              {"benchmark":"c","runner":"pipit","mode":"innerloop","runs":3,"median_nanos":20}]}`)
	rerun := decodeReport(t, `{"generated_at_utc":"t2","host":{"os":"linux","arch":"amd64","cpu_count_logical":8},
		"runs":[{"benchmark":"b","runner":"pipit","mode":"innerloop","status":"ok"}],
		"aggregates":[{"benchmark":"b","runner":"pipit","mode":"innerloop","runs":5,"median_nanos":10}]}`)

	rows := convert([]report{sweep, rerun})

	require.Len(t, rows, 2, "the rerun drops pipit's c row and keeps cpython")
	assert.Equal(t, "pipit", rows[0].Lang)
	assert.Equal(t, int64(10), rows[0].RuntimeNs)
	assert.Equal(t, "t2", rows[0].Timestamp)
	assert.Equal(t, "cpython", rows[1].Lang)
	assert.Equal(t, int64(30), rows[1].RuntimeNs)
}

func TestConvertFailedRunGivesFailedRow(t *testing.T) {
	input := decodeReport(t, `{"generated_at_utc":"t","host":{},
		"runs":[{"benchmark":"b","runner":"yaegi","mode":"endtoend","status":"ok"},
		        {"benchmark":"b","runner":"yaegi","mode":"endtoend","status":"error","note":"timeout"},
		        {"benchmark":"c","runner":"yaegi","mode":"endtoend","status":"ok"}],
		"aggregates":[{"benchmark":"b","runner":"yaegi","mode":"endtoend","runs":1,"median_nanos":5}]}`)

	rows := convert([]report{input})

	require.Len(t, rows, 2)
	assert.Equal(t, row{Lang: "yaegi", Bench: "b", Mode: "endtoend", Status: "failed", Reason: "timeout", Timestamp: "t"}, rows[0])
	assert.Equal(t, "failed", rows[1].Status, "no aggregate")
	assert.Equal(t, "failed", rows[1].Reason)
}

func TestWriteMatchesSweepFormat(t *testing.T) {
	var buffer bytes.Buffer
	require.NoError(t, write(&buffer, []row{{Lang: "go", Bench: "b", Mode: "innerloop", Status: "ok", RuntimeNs: 7}}))
	assert.Equal(t, `{"lang":"go","bench":"b","mode":"innerloop","status":"ok","reason":"","runs":0,"compile_ns":0,"runtime_ns":7,"cold_start_ns":0,"mean_ns":0,"stddev_ns":0,"min_ns":0,"p95_ns":0,"peak_rss_kb":0,"timestamp":"","host_os":"","host_arch":"","go_version":"","cpu_count_logical":0}`+"\n", buffer.String())
}
