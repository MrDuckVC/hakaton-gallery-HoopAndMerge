//go:build !unix

package photofiles

import "os"

func openPhotoFile(root *os.Root, name string) (*os.File, error) { return root.Open(name) }
