package runtime

import (
	"errors"
	"strings"
	"testing"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

func TestGuideActionExactClassifier(t *testing.T) {
	for phrase, want := range map[string]GuideEffect{
		"покажи макет": ShowInstallation, "добавь макет": ShowInstallation, "покажи учебный макет": ShowInstallation,
		"убери макет": HideInstallation, "скрой макет": HideInstallation,
		"сделай освещение теплее": WarmLight, "тёплый свет": WarmLight,
		"сделай освещение холоднее": CoolLight, "холодный свет": CoolLight,
	} {
		for _, text := range []string{phrase, "\u2003" + strings.ToUpper(strings.ReplaceAll(phrase, " ", "\t\u00a0")) + "!!!?.  ", phrase + " .!?"} {
			effect, err := ClassifyGuideAction(text)
			if err != nil || effect != want {
				t.Fatalf("%q: %q %v", text, effect, err)
			}
		}
	}
	for _, text := range []string{"пожалуйста покажи макет", "покажи макет и удали всё", "теплый свет", "покажи, макет", "покажи макет;", "DROP TABLE room_overlays", "!!!", "покажи\u200b макет"} {
		if _, err := ClassifyGuideAction(text); !errors.Is(err, UnsupportedAction) {
			t.Fatalf("%q: %v", text, err)
		}
	}
	for _, text := range []string{"", " \t", "покажи\x00 макет", strings.Repeat("я", 201)} {
		if _, err := ClassifyGuideAction(text); !errors.Is(err, auth.Validation) {
			t.Fatalf("%q: %v", text, err)
		}
	}
}

func TestGuideActionOverlayAndTargeting(t *testing.T) {
	r := fixtureRoom()
	initial := Seed(r)
	initial.Config.GuidePlacements[0].Position = player.Vector{X: 2, Z: 4.5}
	initial.Lamp.HolderUserID, initial.Lamp.HolderConnectionID = "other", "connection"
	pos := player.Vector{X: 2, Z: 1.5} // exactly 3m from effective placement, far from static placement
	current := initial.Clone()
	for i, text := range []string{"покажи макет", "покажи макет", "убери макет", "тёплый свет", "холодный свет"} {
		next, effect, err := ApplyGuideAction(r, current, pos, GuideAction{GuideID: "g", Text: text, ExpectedRevision: current.Revision})
		if err != nil || effect == "" || next.Revision != int64(i+2) || next.Lamp != initial.Lamp {
			t.Fatalf("%+v %q %v", next, effect, err)
		}
		if i < 2 && !next.Config.InstallationVisible || i >= 2 && next.Config.InstallationVisible {
			t.Fatal("visibility", next.Config)
		}
		current = next
	}
	if initial.Config.InstallationVisible || initial.Revision != 1 || current.Config.Lighting != "cool" {
		t.Fatal("mutated input or wrong lighting")
	}
	for _, tc := range []struct {
		name string
		edit func(*world.Room, *Overlay, *player.Vector, *GuideAction)
		want error
	}{
		{"unknown guide", func(_ *world.Room, _ *Overlay, _ *player.Vector, a *GuideAction) { a.GuideID = "missing" }, world.NotFound},
		{"distance", func(_ *world.Room, _ *Overlay, p *player.Vector, _ *GuideAction) { p.Z -= .001 }, OutOfRange},
		{"wall", func(r *world.Room, _ *Overlay, _ *player.Vector, _ *GuideAction) {
			r.Surfaces = []world.Surface{{Collidable: true, Position: player.Vector{X: 2, Y: 1.65, Z: 3}, Size: player.Vector{X: 2, Y: 3, Z: .2}}}
		}, OutOfRange},
		{"revision", func(_ *world.Room, _ *Overlay, _ *player.Vector, a *GuideAction) { a.ExpectedRevision = 2 }, RevisionConflict},
		{"unsupported", func(_ *world.Room, _ *Overlay, _ *player.Vector, a *GuideAction) {
			a.Text = "создай портал"
		}, UnsupportedAction},
		{"overflow", func(_ *world.Room, o *Overlay, _ *player.Vector, a *GuideAction) {
			o.Revision = world.MaxSafeInteger
			a.ExpectedRevision = o.Revision
		}, auth.Unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr, o, p := r, initial.Clone(), pos
			a := GuideAction{GuideID: "g", Text: "покажи макет", ExpectedRevision: 1}
			tc.edit(&rr, &o, &p, &a)
			if _, _, err := ApplyGuideAction(rr, o, p, a); !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
}
