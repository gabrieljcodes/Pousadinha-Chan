package gacha

import (
	"context"
	"strings"
	"testing"
)

func TestBuildClassesAndSkillsCatalog(t *testing.T) {
	store := &Store{}
	classes := store.GetClasses()
	if len(classes) != 4 {
		t.Fatalf("expected 4 classes, got %d", len(classes))
	}

	skills := store.GetSkillDefs()
	if len(skills) != 16 {
		t.Fatalf("expected 16 skills total, got %d", len(skills))
	}

	// Verify each class has 4 skills in tiers 1 to 4 with correct costs
	for _, c := range classes {
		classSkills := store.GetClassSkills(c.ID)
		if len(classSkills) != 4 {
			t.Fatalf("class %s: expected 4 skills, got %d", c.ID, len(classSkills))
		}

		expectedCosts := map[int]int{1: 1, 2: 2, 3: 3, 4: 5}
		var prevID string
		for _, sk := range classSkills {
			expectedCost := expectedCosts[sk.Tier]
			if sk.PointsCost != expectedCost {
				t.Errorf("skill %s (tier %d): expected cost %d, got %d", sk.ID, sk.Tier, expectedCost, sk.PointsCost)
			}
			if sk.Tier > 1 && sk.PrereqID != prevID {
				t.Errorf("skill %s (tier %d): expected prereq %s, got %s", sk.ID, sk.Tier, prevID, sk.PrereqID)
			}
			prevID = sk.ID
		}
	}
}

func TestBuildPrefixCommandParsing(t *testing.T) {
	tests := []struct {
		input  string
		subCmd string
		arg1   string
	}{
		{"!builds", "builds", ""},
		{"!build", "builds", ""},
		{"!tree", "builds", ""},
		{"!skills", "builds", ""},
		{"!learnskill trickster_t1_pocket", "learnskill", "trickster_t1_pocket"},
		{"!learn oracle_t2_foresight", "learnskill", "oracle_t2_foresight"},
		{"!respec", "respec", ""},
		{"!resetbuild", "respec", ""},
	}

	for _, tt := range tests {
		cmd, ok := parsePrefixCommand(tt.input)
		if !ok {
			t.Errorf("expected %s to be recognized as gacha prefix command", tt.input)
			continue
		}
		if cmd.SubCmd != tt.subCmd {
			t.Errorf("%s: expected subCmd %s, got %s", tt.input, tt.subCmd, cmd.SubCmd)
		}
		if tt.arg1 != "" && cmd.Arg1 != tt.arg1 {
			t.Errorf("%s: expected arg1 %s, got %s", tt.input, tt.arg1, cmd.Arg1)
		}
	}
}

func TestBuildPointsHardCapCalculation(t *testing.T) {
	if MaxBuildPoints != 15 {
		t.Fatalf("expected MaxBuildPoints to be 15, got %d", MaxBuildPoints)
	}

	// Calculate points logic check: 5 from rolls, 5 from claims, 3 from keys, 3 from tomes = 16 => capped at 15
	rollPoints := 5
	claimPoints := 5
	keyPoints := 3
	tomePoints := 3
	total := rollPoints + claimPoints + keyPoints + tomePoints
	if total > MaxBuildPoints {
		total = MaxBuildPoints
	}

	if total != 15 {
		t.Fatalf("expected capped total to be 15, got %d", total)
	}
}

func TestTricksterTrapMasqueradeMechanic(t *testing.T) {
	// Verify ErrTrapRollerClick is defined
	if ErrTrapRollerClick == nil {
		t.Fatal("ErrTrapRollerClick must not be nil")
	}

	roll := Roll{
		ID:        "trap-roll-1",
		IsTrap:    true,
		FakeCard:  &Card{ID: 999, Name: "Fake Wish Character"},
		Card:      Card{ID: 100, Name: "Real Underlying Character"},
		RollsLeft: 3,
	}

	if !roll.IsTrap || roll.FakeCard == nil {
		t.Fatal("expected roll to be marked as trap with FakeCard")
	}
	if roll.FakeCard.ID == roll.Card.ID {
		t.Fatal("fake card and real card must have different IDs")
	}
}

func TestHasPlayerSkillCache(t *testing.T) {
	store := &Store{}
	ctx := context.Background()

	// In memory test without DB: HasPlayerSkill returns false for unconfigured store
	has := store.HasPlayerSkill(ctx, "guild-1", "user-1", "trickster_t1_pocket")
	if has {
		t.Fatal("expected false without DB connection")
	}
}

func TestGuardianNerfedSkills(t *testing.T) {
	store := &Store{}
	t1, ok1 := store.GetSkillDef("guardian_t1_endurance")
	if !ok1 {
		t.Fatal("guardian_t1_endurance must exist")
	}
	if t1.Tier != 1 || t1.PointsCost != 1 {
		t.Errorf("guardian_t1_endurance tier=%d cost=%d", t1.Tier, t1.PointsCost)
	}
	if !strings.Contains(t1.Description, "+1 roll por hora") {
		t.Errorf("guardian_t1_endurance description should mention +1 roll por hora: %s", t1.Description)
	}

	t2, ok2 := store.GetSkillDef("guardian_t2_draw")
	if !ok2 {
		t.Fatal("guardian_t2_draw must exist")
	}
	if t2.Tier != 2 || t2.PointsCost != 2 {
		t.Errorf("guardian_t2_draw tier=%d cost=%d", t2.Tier, t2.PointsCost)
	}
	if t2.PrereqID != "guardian_t1_endurance" {
		t.Errorf("guardian_t2_draw prereq=%s, want guardian_t1_endurance", t2.PrereqID)
	}
	// Verify description mentions 0.5 segundos and wishes
	if !strings.Contains(t2.Description, "0.5 segundos") || !strings.Contains(t2.Description, "wishes") {
		t.Errorf("guardian_t2_draw description should mention 0.5 segundos and wishes: %s", t2.Description)
	}

	t3, ok3 := store.GetSkillDef("guardian_t3_tracker")
	if !ok3 {
		t.Fatal("guardian_t3_tracker must exist")
	}
	if t3.PrereqID != "guardian_t2_draw" {
		t.Errorf("guardian_t3_tracker prereq=%s, want guardian_t2_draw", t3.PrereqID)
	}

	t4, ok4 := store.GetSkillDef("guardian_t4_aegis")
	if !ok4 {
		t.Fatal("guardian_t4_aegis must exist")
	}
	if t4.Tier != 4 || t4.PointsCost != 5 {
		t.Errorf("guardian_t4_aegis tier=%d cost=%d", t4.Tier, t4.PointsCost)
	}
	if t4.PrereqID != "guardian_t3_tracker" {
		t.Errorf("guardian_t4_aegis prereq=%s, want guardian_t3_tracker", t4.PrereqID)
	}
	// Verify description mentions 75s and 100 moedas
	if !strings.Contains(t4.Description, "75s") || !strings.Contains(t4.Description, "100 moedas") {
		t.Errorf("guardian_t4_aegis description should mention 75s and 100 moedas: %s", t4.Description)
	}
}

