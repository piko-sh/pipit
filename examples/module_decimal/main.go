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

	"github.com/shopspring/decimal"
)

type LineItem struct {
	Name     string
	Quantity int
	UnitCost string
}

func main() {
	items := []LineItem{
		{Name: "Mechanical keyboard", Quantity: 1, UnitCost: "129.99"},
		{Name: "USB-C cable", Quantity: 3, UnitCost: "8.49"},
		{Name: "Coffee beans (kg)", Quantity: 2, UnitCost: "24.50"},
	}

	subtotal := decimal.Zero
	for _, item := range items {
		unit := decimal.RequireFromString(item.UnitCost)
		lineTotal := unit.Mul(decimal.NewFromInt(int64(item.Quantity)))
		subtotal = subtotal.Add(lineTotal)
		fmt.Printf("  %-25s  %d x %s = %s\n", item.Name, item.Quantity, unit.StringFixed(2), lineTotal.StringFixed(2))
	}

	taxRate := decimal.NewFromFloat(0.20)
	tax := subtotal.Mul(taxRate)
	total := subtotal.Add(tax)

	fmt.Println("------------------------------------------------------------")
	fmt.Printf("  %-25s %16s\n", "Subtotal:", subtotal.StringFixed(2))
	fmt.Printf("  %-25s %16s\n", "VAT (20%):", tax.StringFixed(2))
	fmt.Printf("  %-25s %16s\n", "TOTAL:", total.StringFixed(2))

	third := decimal.NewFromInt(1).Div(decimal.NewFromInt(3))
	fmt.Println()
	fmt.Printf("1 / 3 to 20 dp = %s\n", third.StringFixed(20))

	a := decimal.RequireFromString("0.1")
	b := decimal.RequireFromString("0.2")
	sum := a.Add(b)
	fmt.Printf("0.1 + 0.2 == 0.3 : %v  (sum = %s, no float error)\n",
		sum.Equal(decimal.RequireFromString("0.3")), sum.String())
}
