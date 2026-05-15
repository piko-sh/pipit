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

	"github.com/davecgh/go-spew/spew"
)

type Address struct {
	Street string
	City   string
	Zip    string
}

type Customer struct {
	ID      int
	Name    string
	Email   string
	Address Address
	Tags    []string
	Active  bool
}

type Order struct {
	ID         string
	Customer   Customer
	Items      []string
	Quantities map[string]int
	Total      float64
}

func main() {
	order := Order{
		ID: "ORD-2024-1138",
		Customer: Customer{
			ID:    7,
			Name:  "Ada Lovelace",
			Email: "ada@analyticalengine.org",
			Address: Address{
				Street: "10 Downing Street",
				City:   "London",
				Zip:    "SW1A 2AA",
			},
			Tags:   []string{"vip", "first-mover", "uk"},
			Active: true,
		},
		Items:      []string{"slide-rule", "punch-cards", "tea"},
		Quantities: map[string]int{"slide-rule": 1, "punch-cards": 200, "tea": 50},
		Total:      129.99,
	}

	fmt.Println("== spew.Dump (verbose, types + addresses) ==")
	spew.Dump(order)

	fmt.Println("\n== spew.Sdump (string variant) ==")
	out := spew.Sdump(order.Customer.Address)
	fmt.Print(out)

	fmt.Println("\n== spew.Printf %#+v ==")
	spew.Printf("order = %#+v\n", order.Items)

	fmt.Println("\n== Configured: MaxDepth=1 hides the nested struct ==")
	config := spew.ConfigState{MaxDepth: 1, Indent: "  "}
	config.Dump(order)
}
