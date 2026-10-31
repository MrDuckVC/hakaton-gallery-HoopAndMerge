package photofiles

import (
	"context"
	"io"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/photo"
)

// Read opens only known photo pair names beneath the process-owned root.
func (f *Files) Read(ctx context.Context, id string, thumbnail bool) ([]byte, error) {
	if !photo.UUIDv4(id) {
		return nil, auth.Validation
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name, limit := id+"/image", int64(photo.MaxImageBytes)
	if thumbnail {
		name, limit = id+"/thumbnail.jpg", photo.MaxThumbnailBytes
	}
	before, err := f.root.Stat(name)
	if err != nil {
		return nil, storageError("stat photo file", err)
	}
	if !before.Mode().IsRegular() || before.Size() < 1 || before.Size() > limit {
		return nil, auth.Unavailable
	}
	file, err := openPhotoFile(f.root, name)
	if err != nil {
		return nil, storageError("open photo file", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, storageError("inspect photo file", err)
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > limit {
		return nil, auth.Unavailable
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, storageError("read photo file", err)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil {
		return nil, storageError("recheck photo file", err)
	}
	if int64(len(data)) != info.Size() || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return nil, auth.Unavailable
	}
	return data, nil
}
