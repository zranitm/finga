// Package shop declares errors for the error-table check: two that need a row, and two it passes over.
package shop

import "errors"

var (
	ErrSoldOut = errors.New("sold out")
	ErrClosed  = errors.New("closed")
	errHidden  = errors.New("unexported, so never sent")
	ErrCount   = 3 // not an error
)
