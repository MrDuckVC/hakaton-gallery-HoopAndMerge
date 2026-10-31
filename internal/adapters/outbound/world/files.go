package world

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/player"
	domain "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/domain/world"
	usecase "github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/usecase/world"
)

// Files reads server-only catalog metadata and exact immutable wire files under Dir.
type Files struct{ Dir string }
type reader struct {
	root  *os.Root
	infos []fs.FileInfo
}

func (r *reader) read(ctx context.Context, path string) (usecase.File, error) {
	if err := ctx.Err(); err != nil {
		return usecase.File{}, err
	}
	if !fs.ValidPath(path) || strings.Contains(path, "\\") {
		return usecase.File{}, errors.New("invalid world-relative path")
	}
	f, err := r.root.Open(path)
	if err != nil {
		return usecase.File{}, fmt.Errorf("open world file: %w", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return usecase.File{}, err
	}
	if !info.Mode().IsRegular() {
		return usecase.File{}, errors.New("world file must be regular")
	}
	identity := -1
	for i, other := range r.infos {
		if os.SameFile(info, other) {
			identity = i
			break
		}
	}
	if identity < 0 {
		identity = len(r.infos)
		r.infos = append(r.infos, info)
	}
	var b bytes.Buffer
	chunk := make([]byte, 64*1024)
	for {
		if contextErr := ctx.Err(); contextErr != nil {
			return usecase.File{}, contextErr
		}
		n, readErr := f.Read(chunk)
		if n > 0 {
			_, _ = b.Write(chunk[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return usecase.File{}, fmt.Errorf("read world file: %w", readErr)
		}
	}
	after, err := f.Stat()
	if err != nil {
		return usecase.File{}, err
	}
	if info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) || int64(b.Len()) != info.Size() {
		return usecase.File{}, errors.New("world file changed while reading")
	}
	return usecase.File{Identity: fmt.Sprintf("file-%d", identity), Bytes: b.Bytes()}, nil
}

// Load parses wire DTOs at the boundary, preserving every room descriptor byte.
func (f Files) Load(ctx context.Context) (usecase.LoadedWorld, error) {
	var out usecase.LoadedWorld
	root, err := os.OpenRoot(f.Dir)
	if err != nil {
		return out, fmt.Errorf("open world root: %w", err)
	}
	defer func() { _ = root.Close() }()
	r := reader{root: root}
	indexFile, err := r.read(ctx, "catalog.json")
	if err != nil {
		return out, err
	}
	var idx index
	if err := decode(indexFile.Bytes, &idx); err != nil {
		return out, fmt.Errorf("catalog.json: %w", err)
	}
	out = usecase.LoadedWorld{Input: domain.Input{ID: idx.ID, Version: idx.Version, Start: idx.Start.domain(idx.Version), Spawns: make(map[string]player.State)}, Rooms: make(map[string]usecase.File), Assets: make(map[string]usecase.File)}
	for id, p := range idx.Spawns {
		out.Input.Spawns[id] = p.domain(idx.Version)
	}
	for _, ref := range idx.Rooms {
		if !domain.ValidID(ref.ID) || out.Rooms[ref.ID].Identity != "" {
			return out, errors.New("invalid or duplicate room reference")
		}
		file, err := r.read(ctx, ref.File)
		if err != nil {
			return out, err
		}
		var dto room
		if err := decode(file.Bytes, &dto); err != nil {
			return out, fmt.Errorf("room %q: %w", ref.ID, err)
		}
		if dto.ID != ref.ID {
			return out, errors.New("room ID differs from catalog")
		}
		file.ContentType = "application/json"
		out.Rooms[ref.ID] = file
		out.Input.Rooms = append(out.Input.Rooms, dto.domain())
	}
	for _, ref := range idx.Assets {
		if !domain.ValidID(ref.ID) || out.Assets[ref.ID].Identity != "" {
			return out, errors.New("invalid or duplicate asset reference")
		}
		file, err := r.read(ctx, ref.File)
		if err != nil {
			return out, err
		}
		file.ContentType, err = imageType(ctx, file.Bytes)
		if err != nil {
			return out, fmt.Errorf("asset %q: %w", ref.ID, err)
		}
		out.Assets[ref.ID] = file
		out.Input.Assets = append(out.Input.Assets, ref.ID)
	}
	for _, ref := range idx.Profiles {
		if !domain.ValidID(ref.ID) {
			return out, errors.New("invalid profile ID")
		}
		file, err := r.read(ctx, ref.File)
		if err != nil {
			return out, err
		}
		var profile map[string]any
		if err := decode(file.Bytes, &profile); err != nil {
			return out, fmt.Errorf("profile %q: %w", ref.ID, err)
		}
		if len(profile) == 0 {
			return out, errors.New("empty guide profile")
		}
		out.Input.Profiles = append(out.Input.Profiles, ref.ID)
	}
	return out, nil
}
