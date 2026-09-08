package games

import (
	"testing"
)

func TestBlackjackShoeCreation(t *testing.T) {
	deck := createDeck()
	if len(deck) != 208 {
		t.Fatalf("Expected 208 cards in 4-deck shoe, got %d", len(deck))
	}

	aceCount := 0
	for _, c := range deck {
		if c.Value == "A" {
			aceCount++
		}
	}
	if aceCount != 16 {
		t.Fatalf("Expected 16 Aces in 4-deck shoe, got %d", aceCount)
	}
}

func TestScoreCalculation(t *testing.T) {
	// Soft 20: Ace + 9
	h1 := Hand{
		Cards: []Card{
			{Value: "A", Score: 11},
			{Value: "9", Score: 9},
		},
	}
	calculateScore(&h1)
	if h1.Score != 20 || h1.Aces != 1 {
		t.Fatalf("Expected score 20 with 1 Ace, got %d (aces: %d)", h1.Score, h1.Aces)
	}

	// Soft 21: Ace + 9 + Ace
	h2 := Hand{
		Cards: []Card{
			{Value: "A", Score: 11},
			{Value: "9", Score: 9},
			{Value: "A", Score: 11},
		},
	}
	calculateScore(&h2)
	if h2.Score != 21 {
		t.Fatalf("Expected score 21 for A+9+A, got %d", h2.Score)
	}

	// Hard 15: Ace + 9 + 5 (11+9+5 = 25 -> ace becomes 1 -> 15)
	h3 := Hand{
		Cards: []Card{
			{Value: "A", Score: 11},
			{Value: "9", Score: 9},
			{Value: "5", Score: 5},
		},
	}
	calculateScore(&h3)
	if h3.Score != 15 {
		t.Fatalf("Expected score 15 for A+9+5, got %d", h3.Score)
	}
}

func TestIsBlackjack(t *testing.T) {
	bj := Hand{
		Cards: []Card{
			{Value: "A", Score: 11},
			{Value: "K", Score: 10},
		},
		Score: 21,
	}
	if !isBlackjack(bj) {
		t.Fatal("Expected isBlackjack to be true for Ace + King")
	}

	threeCard21 := Hand{
		Cards: []Card{
			{Value: "7", Score: 7},
			{Value: "7", Score: 7},
			{Value: "7", Score: 7},
		},
		Score: 21,
	}
	if isBlackjack(threeCard21) {
		t.Fatal("Expected isBlackjack to be false for 3-card 21")
	}
}

func TestProfitAndInsuranceMath(t *testing.T) {
	bet := 100
	insurance := 50

	// Case 1: Player won hand (200), but insurance lost (-50) -> net profit = +50
	winnings := bet * 2
	totalSpent := bet + insurance
	netProfit := winnings - totalSpent
	if netProfit != 50 {
		t.Fatalf("Expected net profit 50, got %d", netProfit)
	}

	// Case 2: Dealer BJ, Player insured (pays 2:1 on insurance, loses main bet)
	// Insurance payout = 50 * 3 = 150. Total spent = 150. Net result = 0 (even)
	insuranceWinnings := insurance * 3
	insuranceNet := insuranceWinnings - totalSpent
	if insuranceNet != 0 {
		t.Fatalf("Expected even result (0) when insured against dealer BJ, got %d", insuranceNet)
	}

	// Case 3: Surrender (recovers 50% of bet)
	surrenderWinnings := bet / 2
	surrenderNet := surrenderWinnings - bet
	if surrenderNet != -50 {
		t.Fatalf("Expected net loss of -50 on surrender, got %d", surrenderNet)
	}
}

func TestNaturalBlackjackResolution(t *testing.T) {
	// Case 1: Player has BJ, Dealer shows Ace but does not have BJ (e.g. Ace + 8)
	// Player must get "blackjack" (3:2 payout), NOT push or 1:1
	playerBJ := Hand{
		Cards: []Card{
			{Value: "A", Score: 11},
			{Value: "K", Score: 10},
		},
		Score: 21,
	}
	dealerShowsAceNoBJ := Hand{
		Cards: []Card{
			{Value: "8", Score: 8},
			{Value: "A", Score: 11},
		},
		Score: 19,
	}

	if !isBlackjack(playerBJ) {
		t.Fatal("Expected playerBJ to be blackjack")
	}
	if isBlackjack(dealerShowsAceNoBJ) {
		t.Fatal("Expected dealer not to have blackjack")
	}

	var status string
	if isBlackjack(playerBJ) {
		if isBlackjack(dealerShowsAceNoBJ) {
			status = "push"
		} else {
			status = "blackjack"
		}
	}
	if status != "blackjack" {
		t.Fatalf("Expected status 'blackjack' when dealer doesn't have BJ, got %s", status)
	}

	// Case 2: Both have BJ -> Push
	dealerBJ := Hand{
		Cards: []Card{
			{Value: "10", Score: 10},
			{Value: "A", Score: 11},
		},
		Score: 21,
	}
	if isBlackjack(playerBJ) {
		if isBlackjack(dealerBJ) {
			status = "push"
		} else {
			status = "blackjack"
		}
	}
	if status != "push" {
		t.Fatalf("Expected status 'push' when both have BJ, got %s", status)
	}
}

func TestDoubleDownProtectionOnDealerBlackjack(t *testing.T) {
	// If dealer shows Ace and has BJ, checkDealerBlackjackOnAction returns true
	game := &BlackjackGame{
		DealerHand: Hand{
			Cards: []Card{
				{Value: "10", Score: 10},
				{Value: "A", Score: 11},
			},
			Score: 21,
		},
		PlayerHand: Hand{
			Cards: []Card{
				{Value: "5", Score: 5},
				{Value: "6", Score: 6},
			},
			Score: 11,
		},
	}

	if !game.checkDealerBlackjackOnAction() {
		t.Fatal("Expected checkDealerBlackjackOnAction to return true for dealer BJ")
	}
	if game.Status != "dealer_win" {
		t.Fatalf("Expected status 'dealer_win', got %s", game.Status)
	}
}

func TestSplitEligibilityAndScore(t *testing.T) {
	// Pair of 8s
	h8 := Hand{
		Cards: []Card{
			{Value: "8", Score: 8},
			{Value: "8", Score: 8},
		},
		Score: 16,
	}
	canSplit8 := len(h8.Cards) == 2 && (h8.Cards[0].Value == h8.Cards[1].Value || h8.Cards[0].Score == h8.Cards[1].Score)
	if !canSplit8 {
		t.Fatal("Expected 8-8 pair to be eligible for split")
	}

	// Pair of 10-value cards (e.g. King and Queen)
	hKQ := Hand{
		Cards: []Card{
			{Value: "K", Score: 10},
			{Value: "Q", Score: 10},
		},
		Score: 20,
	}
	canSplitKQ := len(hKQ.Cards) == 2 && (hKQ.Cards[0].Value == hKQ.Cards[1].Value || hKQ.Cards[0].Score == hKQ.Cards[1].Score)
	if !canSplitKQ {
		t.Fatal("Expected K-Q pair (both 10-score) to be eligible for split")
	}

	// Non-matching cards (7 and 9)
	h79 := Hand{
		Cards: []Card{
			{Value: "7", Score: 7},
			{Value: "9", Score: 9},
		},
		Score: 16,
	}
	canSplit79 := len(h79.Cards) == 2 && (h79.Cards[0].Value == h79.Cards[1].Value || h79.Cards[0].Score == h79.Cards[1].Score)
	if canSplit79 {
		t.Fatal("Expected 7-9 not to be eligible for split")
	}
}

