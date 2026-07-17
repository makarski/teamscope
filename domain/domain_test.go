package domain

import "testing"

func TestRubricKeysAndFind(t *testing.T) {
	r := Rubric{Name: "readiness", Criteria: []Criterion{
		{Key: "security", Title: "Security"},
		{Key: "pricing", Title: "Pricing"},
	}}

	keys := r.Keys()
	if len(keys) != 2 {
		t.Fatalf("Keys len = %d, want 2: %v", len(keys), keys)
	}
	if keys[0] != "security" {
		t.Fatalf("Keys[0] = %q, want security", keys[0])
	}
	if keys[1] != "pricing" {
		t.Fatalf("Keys[1] = %q, want pricing", keys[1])
	}

	c, ok := r.Find("pricing")
	if !ok || c.Title != "Pricing" {
		t.Fatalf("Find(pricing) = %+v, %v", c, ok)
	}
	if _, ok := r.Find("missing"); ok {
		t.Fatalf("Find(missing) should be false")
	}
}

func TestSnapshotMix(t *testing.T) {
	s := Snapshot{Epics: []ClassifiedEpic{
		{Criterion: CriterionRef{Key: "security"}},
		{Criterion: CriterionRef{Key: "security"}},
		{Criterion: CriterionRef{Key: "pricing"}},
		{Criterion: CriterionRef{Key: ""}}, // unmapped
	}}

	mix := s.Mix()
	if got := mix["security"]; got != 0.5 {
		t.Errorf("security share = %v, want 0.5", got)
	}
	if got := mix["pricing"]; got != 0.25 {
		t.Errorf("pricing share = %v, want 0.25", got)
	}
	if got := mix[""]; got != 0.25 {
		t.Errorf("unmapped share = %v, want 0.25", got)
	}
}

func TestSnapshotMixEmpty(t *testing.T) {
	s := Snapshot{}
	if mix := s.Mix(); len(mix) != 0 {
		t.Errorf("empty snapshot mix = %v, want empty", mix)
	}
}
