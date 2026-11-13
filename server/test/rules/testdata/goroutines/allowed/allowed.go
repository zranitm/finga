// Package allowed may start goroutines as it likes.
package allowed

// Start starts one.
func Start() { go func() {}() }
