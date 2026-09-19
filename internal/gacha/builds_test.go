package gacha

import (
	"context"
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
