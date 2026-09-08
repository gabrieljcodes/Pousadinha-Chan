package games

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sync"
	"testing"
)

func TestCalculateMinesMultiplier(t *testing.T) {
	// For Total=20, Mines=3, RTP=0.99
	// Pick 1: 0.99 * (20/17) = 1.1647 -> 1.16
	m1 := CalculateMinesMultiplier(20, 3, 1)
	if m1 != 1.16 {
		t.Errorf("Expected pick 1 multiplier 1.16, got %.2f", m1)
	}

	// Pick 2: 0.99 * (20/17) * (19/16) = 1.3831 -> 1.38
	m2 := CalculateMinesMultiplier(20, 3, 2)
	if m2 != 1.38 {
		t.Errorf("Expected pick 2 multiplier 1.38, got %.2f", m2)
	}

	// Pick 3: 0.99 * (20/17) * (19/16) * (18/15) = 1.6597 -> 1.66
	m3 := CalculateMinesMultiplier(20, 3, 3)
	if m3 != 1.66 {
		t.Errorf("Expected pick 3 multiplier 1.66, got %.2f", m3)
	}

	// Pick 0 or negative
	if m0 := CalculateMinesMultiplier(20, 3, 0); m0 != 1.0 {
		t.Errorf("Expected 1.0 for 0 picks, got %.2f", m0)
	}

	// Pick exceeding max available diamonds
	if mOver := CalculateMinesMultiplier(20, 3, 18); mOver != 1.0 {
		t.Errorf("Expected 1.0 for invalid picks, got %.2f", mOver)
	}

	// Multipliers must strictly increase as more diamonds are picked
	prev := 1.0
	for k := 1; k <= 17; k++ {
		cur := CalculateMinesMultiplier(20, 3, k)
		if cur <= prev {
			t.Errorf("Multiplier at pick %d (%.2f) not greater than previous (%.2f)", k, cur, prev)
		}
		prev = cur
	}
}

func TestGenerateMinesBoard(t *testing.T) {
	totalTiles := 20
	for mines := 1; mines <= 19; mines++ {
		board, minePositions := generateMinesBoard(totalTiles, mines)

		if len(board) != totalTiles {
			t.Fatalf("Expected board length %d, got %d", totalTiles, len(board))
		}
		if len(minePositions) != mines {
			t.Fatalf("Expected %d mine positions, got %d", mines, len(minePositions))
		}

		// Check for uniqueness of mine positions
		seen := make(map[int]bool)
		mineCountOnBoard := 0
		for _, pos := range minePositions {
			if pos < 0 || pos >= totalTiles {
				t.Errorf("Mine position %d out of bounds", pos)
			}
			if seen[pos] {
				t.Errorf("Duplicate mine position detected: %d", pos)
			}
			seen[pos] = true
		}

		for _, tile := range board {
			if tile == TileMine {
				mineCountOnBoard++
			}
		}

		if mineCountOnBoard != mines {
			t.Errorf("Board has %d mines, expected %d", mineCountOnBoard, mines)
		}
	}
}

func TestProvablyFairSeed(t *testing.T) {
	minePositions := []int{2, 7, 14}
	seed, hash := generateProvablyFairSeed(minePositions)

	if len(seed) != 32 { // 16 bytes hex = 32 chars
		t.Errorf("Expected seed length 32, got %d", len(seed))
	}

	// Verify that hashing seed:minePositions produces the identical hash
	expectedInput := fmt.Sprintf("%s:%v", seed, minePositions)
	expectedHashBytes := sha256.Sum256([]byte(expectedInput))
	expectedHash := hex.EncodeToString(expectedHashBytes[:])

	if hash != expectedHash {
		t.Errorf("Hash mismatch! Got %s, expected %s", hash, expectedHash)
	}
}

func TestMinesBoardPicksAndWinCalculation(t *testing.T) {
	board := []TileType{
		TileDiamond, TileDiamond, TileMine, TileDiamond, TileDiamond,
		TileDiamond, TileDiamond, TileDiamond, TileDiamond, TileMine,
		TileDiamond, TileDiamond, TileDiamond, TileDiamond, TileDiamond,
		TileDiamond, TileDiamond, TileMine, TileDiamond, TileDiamond,
	}
	minePositions := []int{2, 9, 17}

	game := &MinesGame{
		UserID:            "test_user",
		Bet:               100,
		MinesCount:        3,
		TotalTiles:        20,
		Board:             board,
		Revealed:          make([]bool, 20),
		MinePositions:     minePositions,
		PicksCount:        0,
		CurrentMultiplier: 1.0,
		Status:            "playing",
	}

	// Pick 0: safe diamond
	game.Revealed[0] = true
	game.PicksCount++
	game.CurrentMultiplier = CalculateMinesMultiplier(20, 3, 1)

	if game.CurrentMultiplier != 1.16 {
		t.Errorf("Expected 1.16 multiplier, got %.2f", game.CurrentMultiplier)
	}

	// Test Cashout calculation
	winnings := int(math.Round(float64(game.Bet) * game.CurrentMultiplier))
	if winnings != 116 {
		t.Errorf("Expected winnings 116, got %d", winnings)
	}
	profit := winnings - game.Bet
	if profit != 16 {
		t.Errorf("Expected profit 16, got %d", profit)
	}

	// Test Bomb pick
	bombIndex := 2
	if game.Board[bombIndex] != TileMine {
		t.Errorf("Position %d expected to be mine", bombIndex)
	}
}

func TestMinesGameFullClear(t *testing.T) {
	// If a player clears all 17 diamonds with 3 mines
	maxPicks := 20 - 3
	mult := CalculateMinesMultiplier(20, 3, maxPicks)
	if mult < 1000.0 {
		t.Errorf("Expected massive multiplier for 17 diamonds, got %.2f", mult)
	}
}

func TestMinesConcurrency(t *testing.T) {
	game := &MinesGame{
		UserID:            "concurrent_user",
		Bet:               100,
		MinesCount:        3,
		TotalTiles:        20,
		Board:             make([]TileType, 20),
		Revealed:          make([]bool, 20),
		Status:            "playing",
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(tile int) {
			defer wg.Done()
			game.mu.Lock()
			defer game.mu.Unlock()
			if !game.Revealed[tile] {
				game.Revealed[tile] = true
				game.PicksCount++
			}
		}(i)
	}
	wg.Wait()

	if game.PicksCount != 20 {
		t.Errorf("Expected 20 picks with mutex synchronization, got %d", game.PicksCount)
	}
}
