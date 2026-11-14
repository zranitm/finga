// Package till maps some of shop's errors to rows, as a facade's table does.
package till

import (
	"errors"

	"finga/test/rules/testdata/errortables/shop"
)

type row struct {
	err  error
	code int
}

var rows = []row{
	{shop.ErrSoldOut, 1},
	{errors.New("not shop's"), 2},
}
