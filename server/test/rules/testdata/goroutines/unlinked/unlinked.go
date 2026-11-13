// Package unlinked is in no binary.
package unlinked

// Start starts one.
func Start() { go func() {}() }
