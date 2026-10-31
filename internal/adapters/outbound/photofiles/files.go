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
		return nil, fmt.Errorf("prepare photo directory: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open photo directory: %w", err)
	}
	return &Files{root: root}, nil
}

// Close releases the root after HTTP requests have drained.
func (f *Files) Close() error { return f.root.Close() }

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
		return fmt.Errorf("check photo destination: %w", err)
	}
	stage := ".staging-" + rand.Text()
	if err := f.root.Mkdir(stage, 0700); err != nil {
		return fmt.Errorf("create photo stage: %w", err)
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
		return fmt.Errorf("publish photo pair: %w", err)
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
		return fmt.Errorf("create photo file: %w", err)
	}
	defer func() { result = errors.Join(result, file.Close()) }()
	if _, err = file.Write(data); err != nil {
		return fmt.Errorf("write photo file: %w", err)
	}
	if err = file.Sync(); err != nil {
		return fmt.Errorf("sync photo file: %w", err)
	}
	return ctx.Err()
}
func (f *Files) syncDir(path string) error {
	dir, err := f.root.Open(path)
	if err != nil {
		return fmt.Errorf("open photo directory to sync: %w", err)
	}
	defer func() { _ = dir.Close() }()
	if err = dir.Sync(); err != nil {
		return fmt.Errorf("sync photo directory: %w", err)
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
		return fmt.Errorf("inspect photo cleanup: %w", err)
	}
	if !info.IsDir() {
		return auth.Unavailable
	}
	var result error
	for _, name := range []string{"image", "thumbnail.jpg"} {
		if err = f.root.Remove(dir + "/" + name); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, fmt.Errorf("remove photo file: %w", err))
		}
	}
	if err = f.root.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		result = errors.Join(result, fmt.Errorf("remove photo directory: %w", err))
	}
	return result
}
