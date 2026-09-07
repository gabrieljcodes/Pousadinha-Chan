package games

import (
	"testing"
)

func TestRandomChamberCryptoDistribution(t *testing.T) {
	seen := make(map[int]bool)
	totalSpins := 6000

	for i := 0; i < totalSpins; i++ {
		chamber := randomChamberCrypto()
		if chamber < 1 || chamber > 6 {
			t.Fatalf("Chamber out of bounds: %d (expected 1-6)", chamber)
		}
		seen[chamber] = true
	}

	for c := 1; c <= 6; c++ {
		if !seen[c] {
			t.Fatalf("Chamber %d was never selected in %d rolls", c, totalSpins)
		}
	}
}

func TestRandomCoinTossCrypto(t *testing.T) {
	heads := 0
	total := 2000

	for i := 0; i < total; i++ {
		if randomCoinTossCrypto() {
			heads++
		}
	}

	ratio := float64(heads) / float64(total)
	if ratio < 0.40 || ratio > 0.60 {
		t.Fatalf("Coin toss biased: %.2f", ratio)
	}
}

func TestRussianRouletteEscalatingProgression(t *testing.T) {
	// Test each possible chamber position from 1 to 6
	for chamber := 1; chamber <= 6; chamber++ {
		game := &RussianRouletteGame{
			Player1ID:   "p1",
			Player2ID:   "p2",
			CurrentTurn: "p1",
			Bet:         100,
			Chamber:     chamber,
			CurrentShot: 1,
			Round:       1,
		}

		died := false
		for shot := 1; shot <= 6; shot++ {
			if game.CurrentShot == game.Chamber {
				died = true
				if shot != chamber {
					t.Fatalf("Expected death at shot %d, but died at shot %d", chamber, shot)
				}
				break
			}
			game.CurrentShot++
			game.CurrentTurn = game.getOtherPlayer(game.CurrentTurn)
			game.Round++
		}

		if !died {
			t.Fatalf("Bullet never fired for chamber %d", chamber)
		}
	}
}

func TestRussianRoulettePotAndOtherPlayer(t *testing.T) {
	game := &RussianRouletteGame{
		Player1ID: "alice",
		Player2ID: "bob",
		Bet:       250,
	}

	if game.getOtherPlayer("alice") != "bob" {
		t.Fatalf("Expected other player of alice to be bob, got %s", game.getOtherPlayer("alice"))
	}
	if game.getOtherPlayer("bob") != "alice" {
		t.Fatalf("Expected other player of bob to be alice, got %s", game.getOtherPlayer("bob"))
	}

	totalPot := game.Bet * 2
	if totalPot != 500 {
		t.Fatalf("Expected total pot 500, got %d", totalPot)
	}
}
