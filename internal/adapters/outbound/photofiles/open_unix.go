//go:build unix

package photofiles

import (
	"os"
	"syscall"
)

// Nonblocking open prevents a FIFO replacement racing with the initial stat from hanging a request.
func openPhotoFile(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
