//
// Copyright (c) 2020 Markku Rossi
//
// All rights reserved.
//

package compiler

import (
	"fmt"
	"testing"
)

type SSAGenTest struct {
	Name string
	Code string
}

var ssagenTests = []SSAGenTest{
	SSAGenTest{
		Name: "Add",
		Code: `
package main
func main(a, b int) int {
    return a + b
}
`,
	},
	SSAGenTest{
		Name: "2 Return Values",
		Code: `
package main
func main(a, b int) (int, int) {
    return a + b, a - b
}
`,
	},
	SSAGenTest{
		Name: "If",
		Code: `
package main
func main(a, b int) int {
    if a > b {
        return a
    }
    return b
}
`,
	},
	SSAGenTest{
		Name: "If-Else",
		Code: `
package main
func main(a, b int) int {
    if a > b {
        return a
    } else {
        return b
    }
}
`,
	},
}

func TestSSAGen(t *testing.T) {
	for idx, test := range ssagenTests {
		fmt.Printf(`==================================================
// Test '%s':
%s--------------------------------------------------
`,
			test.Name, test.Code)
		_, err := Compile(test.Code)
		if err != nil {
			t.Fatalf("SSA test %d failed: %s", idx, err)
		}
	}
}