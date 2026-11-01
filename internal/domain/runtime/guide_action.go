package runtime

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/auth"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/player"
	"github.com/MrDuckVC/hakaton-gallery-HopeAndMerge/internal/domain/world"
)

// GuideEffect is one of the four locally recognized exhibition changes.
type GuideEffect string

// Supported effects never execute model output or arbitrary instructions.
const (
	ShowInstallation  GuideEffect = "show_installation"
	HideInstallation  GuideEffect = "hide_installation"
	WarmLight         GuideEffect = "warm_light"
	CoolLight         GuideEffect = "cool_light"
	UnsupportedAction Code        = "UNSUPPORTED_ACTION"
)

// GuideAction is an exhibition request, independent of educational questions.
type GuideAction struct {
	GuideID, Text    string
	ExpectedRevision int64
}

// ClassifyGuideAction recognizes only the exact Sunday phrases after normalization.
func ClassifyGuideAction(text string) (GuideEffect, error) {
	text = strings.TrimSpace(text)
	if !utf8.ValidString(text) || text == "" || utf8.RuneCountInString(text) > 200 || strings.ContainsRune(text, 0) {
		return "", auth.Validation
	}
	text = strings.TrimSpace(strings.TrimRight(strings.Join(strings.Fields(strings.ToLower(text)), " "), ".!?"))
	switch text {
	case "покажи макет", "добавь макет", "покажи учебный макет":
		return ShowInstallation, nil
	case "убери макет", "скрой макет":
		return HideInstallation, nil
	case "сделай освещение теплее", "тёплый свет":
		return WarmLight, nil
	case "сделай освещение холоднее", "холодный свет":
		return CoolLight, nil
	default:
		return "", UnsupportedAction
	}
}

// ApplyGuideAction validates effective targeting and returns a detached next overlay.
func ApplyGuideAction(r world.Room, o Overlay, position player.Vector, a GuideAction) (Overlay, GuideEffect, error) {
	if !world.ValidID(a.GuideID) || a.ExpectedRevision < 1 || a.ExpectedRevision > world.MaxSafeInteger {
		return Overlay{}, "", auth.Validation
	}
	var target player.Vector
	found := false
	for _, g := range r.Guides {
		if g.ID == a.GuideID {
			found = true
			target = g.Position
			break
		}
	}
	if !found {
		return Overlay{}, "", world.NotFound
	}
	for _, p := range o.Config.GuidePlacements {
		if p.GuideID == a.GuideID {
			target = p.Position
			break
		}
	}
	camera := position
	camera.Y += 1.65
	target.Y += 1.65
	if math.Hypot(camera.X-target.X, camera.Z-target.Z) > 3 || !world.Visible(r, camera, target) {
		return Overlay{}, "", OutOfRange
	}
	effect, err := ClassifyGuideAction(a.Text)
	if err != nil {
		return Overlay{}, "", err
	}
	if a.ExpectedRevision != o.Revision {
		return Overlay{}, "", RevisionConflict
	}
	if o.Revision == world.MaxSafeInteger {
		return Overlay{}, "", auth.Unavailable
	}
	next := o.Clone()
	next.Revision++
	switch effect {
	case ShowInstallation:
		next.Config.InstallationVisible = true
	case HideInstallation:
		next.Config.InstallationVisible = false
	case WarmLight:
		next.Config.Lighting = "warm"
	case CoolLight:
		next.Config.Lighting = "cool"
	}
	return next, effect, nil
}
