// Package photofiles publishes private photo pairs on the local filesystem.
package photofiles

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/photo"
)

// Files confines all operations to a process-owned photo root.
type Files struct{ root *os.Root }

// Open prepares the root without scanning or deleting other uploads.
func Open(dir string) (*Files, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, storageError("prepare photo directory", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, storageError("open photo directory", err)
	}
	return &Files{root: root}, nil
}

// Close releases the root after HTTP requests have drained.
func (f *Files) Close() error { return storageError("close photo directory", f.root.Close()) }

// Save writes and syncs both files before publishing the complete directory in one rename.
func (f *Files) Save(ctx context.Context, id string, original, thumbnail []byte) (result error) {
	if !photo.UUIDv4(id) {
		return auth.Validation
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := f.root.Lstat(id); err == nil {
		return auth.Unavailable
	} else if !errors.Is(err, os.ErrNotExist) {
		return storageError("check photo destination", err)
	}
	stage := ".staging-" + rand.Text()
	if err := f.root.Mkdir(stage, 0700); err != nil {
		return storageError("create photo stage", err)
	}
	published := false
	defer func() {
		if !published {
			result = errors.Join(result, f.removePair(stage))
		}
	}()
	if err := f.write(ctx, stage+"/image", original); err != nil {
		return err
	}
	if err := f.write(ctx, stage+"/thumbnail.jpg", thumbnail); err != nil {
		return err
	}
	if err := f.syncDir(stage); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// A UUID is allocated per attempt; an existing nonempty destination cannot be replaced.
	if err := f.root.Rename(stage, id); err != nil {
		return storageError("publish photo pair", err)
	}
	published = true
	if err := f.syncDir("."); err != nil {
		return errors.Join(err, f.removePair(id))
	}
	return nil
}
func (f *Files) write(ctx context.Context, path string, data []byte) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := f.root.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return storageError("create photo file", err)
	}
	defer func() { result = errors.Join(result, storageError("close photo file", file.Close())) }()
	if _, err = file.Write(data); err != nil {
		return storageError("write photo file", err)
	}
	if err = file.Sync(); err != nil {
		return storageError("sync photo file", err)
	}
	return ctx.Err()
}
func (f *Files) syncDir(path string) (result error) {
	dir, err := f.root.Open(path)
	if err != nil {
		return storageError("open photo directory to sync", err)
	}
	defer func() { result = errors.Join(result, storageError("close photo directory after sync", dir.Close())) }()
	if err = dir.Sync(); err != nil {
		return storageError("sync photo directory", err)
	}
	return nil
}

// Delete removes only the two known files and their owned directory, never a recursive sweep.
func (f *Files) Delete(ctx context.Context, id string) error {
	if !photo.UUIDv4(id) {
		return auth.Validation
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.removePair(id)
}
func (f *Files) removePair(dir string) error {
	info, err := f.root.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return storageError("inspect photo cleanup", err)
	}
	if !info.IsDir() {
		return auth.Unavailable
	}
	var result error
	for _, name := range []string{"image", "thumbnail.jpg"} {
		if err = f.root.Remove(dir + "/" + name); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, storageError("remove photo file", err))
		}
	}
	if err = f.root.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		result = errors.Join(result, storageError("remove photo directory", err))
	}
	return result
}

// storageError preserves the OS cause while classifying operational failures at the adapter boundary.
func storageError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w: %w", operation, auth.Unavailable, err)
}
