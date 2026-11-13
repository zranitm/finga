// Package other declares a rule outside the kernel's package, which the check reports.
package other

import "finga/test/rules/testdata/kernelrules/k"

// ErrStray is a rule declared away from the others.
var ErrStray = k.NewRule("stray")
