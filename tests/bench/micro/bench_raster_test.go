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

//go:build bench

package bench

import "testing"

func BenchmarkRaster(b *testing.B) {
	runHotspotBenchmarks(b, []struct {
		name   string
		source string
	}{
		{"global_byte_field", `package main
 type framebuffer struct { pixels []byte }
 var screen = &framebuffer{make([]byte,4096)}
 func run() byte {
  for i:=0;i<1024;i++ {
   screen.pixels[i*4]=byte(i)
   screen.pixels[i*4+1]=byte(i>>2)
   screen.pixels[i*4+2]=byte(i>>4)
   screen.pixels[i*4+3]=255
  }
  return screen.pixels[2048]
 }`},
		{"value_receiver_byte_view", `package main
 type view struct { data []byte; offset int }
 func (v view) at(i int) byte {
  index:=v.offset+i
  if index>=len(v.data)&&index<len(v.data)+128 {return 0}
  return v.data[index]
 }
 var source=view{make([]byte,1024),0}
 func run() int { n:=0;for i:=0;i<1024;i++ {n+=int(source.at(i))};return n }`},
		{"aggregate_fields", `package main
 type entry struct { red, green, blue, alpha byte }
 var entries=[4]entry{{2,3,5,255},{7,11,13,255},{17,19,23,255},{29,31,37,255}}
 var output=make([]byte,4096)
 func run() byte {
  for i:=0;i<1024;i++ {
   value:=entries[i&3]
   output[i*4]=value.red
   output[i*4+1]=value.green
   output[i*4+2]=value.blue
   output[i*4+3]=255
  }
  return output[2048]
 }`},
		{"receiver_capture", `package main
 type view struct { data []byte; offset int }
 func (v view) at(i int) byte {return v.data[v.offset+i]}
 var source=view{[]byte{2,3,5,7},0}
 func run() int {
  total:=0
  for i:=0;i<1024;i++ {
   copied:=source
   source.offset=(source.offset+1)&3
   total+=int(copied.at(0))
  }
  return total
 }`},
		{"narrow_arithmetic", `package main
 func run() int32 {
  n:=int32(37)
  for i:=int32(0);i<4096;i++ {n=(n+i)*3-7}
  return n
 }`},
	})
}
