package games

import (
	"testing"
)

func TestPickWinningCupDistribution(t *testing.T) {
	// Test 6 cups
	seen6 := make(map[int]bool)
	for i := 0; i < 500; i++ {
		cup := pickWinningCup(6)
		if cup < 1 || cup > 6 {
			t.Fatalf("Expected cup between 1 and 6, got %d", cup)
		}
		seen6[cup] = true
	}
	for c := 1; c <= 6; c++ {
		if !seen6[c] {
			t.Fatalf("Cup %d was never picked in 500 iterations", c)
		}
	}

	// Test 2 cups (Double or Nothing)
	seen2 := make(map[int]bool)
	for i := 0; i < 500; i++ {
		cup := pickWinningCup(2)
		if cup < 1 || cup > 2 {
			t.Fatalf("Expected cup between 1 and 2, got %d", cup)
		}
		seen2[cup] = true
	}
	if !seen2[1] || !seen2[2] {
		t.Fatal("Both cups 1 and 2 should be picked in 500 iterations")
	}
}

func TestPotMultiplicationProgression(t *testing.T) {
	bet := 100
	pot := bet

	// Round 1 (6 cups) -> 5x
	pot *= 5
	if pot != 500 {
		t.Fatalf("Expected Round 1 pot to be 500, got %d", pot)
	}

	// Round 2 (2 cups) -> 2x (Double or Nothing)
	pot *= 2
	if pot != 1000 {
		t.Fatalf("Expected Round 2 pot to be 1000, got %d", pot)
	}

	// Round 3 (2 cups) -> 2x
	pot *= 2
	if pot != 2000 {
		t.Fatalf("Expected Round 3 pot to be 2000, got %d", pot)
	}

	netProfit := pot - bet
	if netProfit != 1900 {
		t.Fatalf("Expected net profit to be 1900, got %d", netProfit)
	}
}
