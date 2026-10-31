package world

import "testing"

func TestGeometryIndependentCopies(t *testing.T) {
	c := load(t)
	r, err := c.Geometry("r0")
	if err != nil {
		t.Fatal(err)
	}
	r.Doors[0].Target = "changed"
	r.Exhibits[0].ID = "changed"
	next, err := c.Geometry("r0")
	if err != nil || next.Doors[0].Target != "r1" || next.Exhibits[0].ID != "er0" {
		t.Fatalf("mutable geometry: %+v %v", next, err)
	}
	if _, err = c.Geometry("missing"); err == nil {
		t.Fatal("missing room accepted")
	}
}
