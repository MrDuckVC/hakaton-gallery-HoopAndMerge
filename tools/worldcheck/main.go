// Command worldcheck validates prepared content with the production catalog and
// prints reproducible exact-byte accounting. It never downloads or edits content.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/inbound/worldwire"
	worldfiles "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/adapters/outbound/world"
	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
	world "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/usecase/world"
)

type report struct {
	WorldID           string        `json:"world_id"`
	WorldVersion      string        `json:"world_version"`
	TotalContentBytes int64         `json:"total_content_bytes"`
	CacheLimitBytes   int64         `json:"client_cache_limit_bytes"`
	PhotoReserveBytes int64         `json:"photo_reserve_bytes"`
	Rooms             []roomReport  `json:"rooms"`
	Assets            []assetReport `json:"assets"`
}
type roomReport struct {
	ID               string `json:"id"`
	DescriptorBytes  int64  `json:"descriptor_bytes"`
	DescriptorSHA256 string `json:"descriptor_sha256"`
	PackageBytes     int64  `json:"package_bytes"`
	HeadroomBytes    int64  `json:"headroom_bytes"`
	MaxSnapshotBytes int64  `json:"max_snapshot_bytes"`
}
type assetReport struct {
	ID     string `json:"id"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// prepared retains the same bytes for validation and reporting, even if files on
// disk change during the command. Validation still belongs to the catalog.
type prepared struct{ world.LoadedWorld }

func (p prepared) Load(ctx context.Context) (world.LoadedWorld, error) {
	return p.LoadedWorld, ctx.Err()
}
func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func run(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("worldcheck", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("dir", "", "local WORLD_DIR")
	version := flags.String("version", "", "expected WORLD_VERSION")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *dir == "" || *version == "" || flags.NArg() != 0 {
		return errors.New("usage: worldcheck -dir WORLD_DIR -version WORLD_VERSION")
	}
	loaded, err := (worldfiles.Files{Dir: *dir}).Load(ctx)
	if err != nil {
		return err
	}
	sizer := worldwire.SnapshotSizer{}
	catalog, err := world.Load(ctx, prepared{loaded}, sizer, *version)
	if err != nil {
		return err
	}
	manifest := catalog.Manifest()
	result := report{WorldID: manifest.ID, WorldVersion: manifest.Version, TotalContentBytes: manifest.TotalBytes, CacheLimitBytes: manifest.CacheLimit, PhotoReserveBytes: domain.PhotoReserve, Rooms: []roomReport{}, Assets: []assetReport{}}
	used := make(map[string]bool)
	for _, r := range loaded.Input.Rooms {
		pkg, err := catalog.Package(r.ID)
		if err != nil {
			return err
		}
		size := pkg.DescriptorBytes
		for _, a := range pkg.Assets {
			size += a.Bytes
			used[a.ID] = true
		}
		snapshot, err := sizer.MaxSnapshotBytes(pkg)
		if err != nil {
			return err
		}
		result.Rooms = append(result.Rooms, roomReport{ID: r.ID, DescriptorBytes: pkg.DescriptorBytes, DescriptorSHA256: digest(loaded.Rooms[r.ID].Bytes), PackageBytes: size, HeadroomBytes: manifest.CacheLimit - size - domain.PhotoReserve, MaxSnapshotBytes: snapshot})
	}
	for id := range used {
		a := loaded.Assets[id]
		result.Assets = append(result.Assets, assetReport{ID: id, Bytes: int64(len(a.Bytes)), SHA256: digest(a.Bytes)})
	}
	sort.Slice(result.Rooms, func(i, j int) bool { return result.Rooms[i].ID < result.Rooms[j].ID })
	sort.Slice(result.Assets, func(i, j int) bool { return result.Assets[i].ID < result.Assets[j].ID })
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	err := run(ctx, os.Args[1:], os.Stdout)
	cancel()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
