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
	"fmt"
	"net/http"
	"strconv"
)

func writeText(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	w.Write([]byte(body))
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeText(w, 200, "ok\n")
}

func handleGreeting(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeText(w, 400, "missing name\n")
		return
	}
	writeText(w, 200, "hello, "+name+"!\n")
}

func handleSquare(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("n")
	n, err := strconv.Atoi(raw)
	if err != nil {
		writeText(w, 400, "n must be an integer\n")
		return
	}
	writeText(w, 200, strconv.Itoa(n*n)+"\n")
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("GET /hello/{name}", handleGreeting)
	mux.HandleFunc("GET /square/{n}", handleSquare)

	address := ":8080"
	fmt.Println("pipit-webserver script: listening on http://localhost" + address)
	fmt.Println("  try: curl http://localhost" + address + "/health")
	fmt.Println("       curl http://localhost" + address + "/hello/world")
	fmt.Println("       curl http://localhost" + address + "/square/12")
	if err := http.ListenAndServe(address, mux); err != nil {
		fmt.Println("server error:", err)
	}
}
