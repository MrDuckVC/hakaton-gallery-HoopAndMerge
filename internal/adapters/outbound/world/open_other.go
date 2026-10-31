//go:build !unix

package world

import "os"

// The caller checks the type before opening and validates the actual descriptor.
func openWorldFile(root *os.Root, name string) (*os.File, error) { return root.Open(name) }
