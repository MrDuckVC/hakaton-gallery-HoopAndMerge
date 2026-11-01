package guide

import (
	"strings"
	"testing"
)

func TestProfileValidationAndMemory(t *testing.T) {
	p := Profile{Personality: "Художник", Style: "Наблюдение", Topic: "Живопись", Constraints: []string{"Не выдумывать цитаты"}, Materials: []Material{{ID: "m", Text: "Свет", Sources: []string{"Локальный каталог"}}}}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Profile){func(p *Profile) { p.Style = "" }, func(p *Profile) { p.Materials = append(p.Materials, p.Materials[0]) }, func(p *Profile) { p.Personality = strings.Repeat("я", 2001) }} {
		copy := p
		copy.Materials = append([]Material(nil), p.Materials...)
		change(&copy)
		if copy.Validate() == nil {
			t.Fatal("invalid profile accepted")
		}
	}
}
