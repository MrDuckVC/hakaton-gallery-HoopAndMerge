//go:build unix

package world

import (
	"os"
	"syscall"
)

// Nonblocking open also handles a file replaced with a FIFO after the initial
// type check. The caller still verifies the actual descriptor and closes it.
func openWorldFile(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
