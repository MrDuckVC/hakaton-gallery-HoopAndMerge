package worldwire

import (
	"strings"
	"testing"

	domain "github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

func TestSnapshotSizeIncludesWorstCaseEnvelope(t *testing.T) {
	p := domain.RoomPackage{RoomID: strings.Repeat(`"`, 128), DescriptorBytes: domain.MaxSafeInteger, Assets: []domain.AssetInfo{{ID: strings.Repeat(`\`, 128), Bytes: domain.MaxSafeInteger, ContentType: "image/jpeg"}}}
	n, err := (SnapshotSizer{}).MaxSnapshotBytes(p)
	if err != nil || n < 40000 || n > domain.MaxMessageBytes {
		t.Fatal(n, err)
	}
	p.Assets = make([]domain.AssetInfo, 1500)
	for i := range p.Assets {
		p.Assets[i] = domain.AssetInfo{ID: strings.Repeat("x", 128), Bytes: domain.MaxSafeInteger, ContentType: "image/webp"}
	}
	n, err = (SnapshotSizer{}).MaxSnapshotBytes(p)
	if err != nil || n <= domain.MaxMessageBytes {
		t.Fatal(n, err)
	}
}
