package games

import (
	"bot/internal/database"
	"testing"
	"time"
)

func TestAnimalDezenaMapping(t *testing.T) {
	// Verify that all 100 dezenas (00 to 99) map to the correct animal
	expectedMap := map[int]int{
		1: 1, 2: 1, 3: 1, 4: 1, // Avestruz
		5: 2, 6: 2, 7: 2, 8: 2, // Águia
		9: 3, 10: 3, 11: 3, 12: 3, // Burro
		13: 4, 14: 4, 15: 4, 16: 4, // Borboleta
		17: 5, 18: 5, 19: 5, 20: 5, // Cachorro
		21: 6, 22: 6, 23: 6, 24: 6, // Cabra
		25: 7, 26: 7, 27: 7, 28: 7, // Carneiro
		29: 8, 30: 8, 31: 8, 32: 8, // Camelo
		33: 9, 34: 9, 35: 9, 36: 9, // Cobra
		37: 10, 38: 10, 39: 10, 40: 10, // Coelho
		41: 11, 42: 11, 43: 11, 44: 11, // Cavalo
		45: 12, 46: 12, 47: 12, 48: 12, // Elefante
		49: 13, 50: 13, 51: 13, 52: 13, // Galo
		53: 14, 54: 14, 55: 14, 56: 14, // Gato
		57: 15, 58: 15, 59: 15, 60: 15, // Jacaré
		61: 16, 62: 16, 63: 16, 64: 16, // Leão
		65: 17, 66: 17, 67: 17, 68: 17, // Macaco
		69: 18, 70: 18, 71: 18, 72: 18, // Porco
		73: 19, 74: 19, 75: 19, 76: 19, // Pavão
		77: 20, 78: 20, 79: 20, 80: 20, // Peru
		81: 21, 82: 21, 83: 21, 84: 21, // Touro
		85: 22, 86: 22, 87: 22, 88: 22, // Tigre
		89: 23, 90: 23, 91: 23, 92: 23, // Urso
		93: 24, 94: 24, 95: 24, 96: 24, // Veado
		97: 25, 98: 25, 99: 25, 0: 25, // Vaca (00 = 25)
	}

	for dezena, expectedGroup := range expectedMap {
		animal := GetAnimalByDezena(dezena)
		if animal.Number != expectedGroup {
			t.Errorf("Dezena %d mapped to group %d (%s), expected group %d",
				dezena, animal.Number, animal.Name, expectedGroup)
		}
	}
}

func TestFindAnimal(t *testing.T) {
	// Search by name (English and Portuguese, case and accent insensitive)
	tests := []struct {
		query    string
		expected int
	}{
		{"monkey", 17},
		{"Monkey", 17},
		{"macaco", 17},
		{"MACACO", 17},
		{"eagle", 2},
		{"Eagle", 2},
		{"aguia", 2},
		{"Águia", 2},
		{"cow", 25},
		{"vaca", 25},
		{"ostrich", 1},
		{"avestruz", 1},
		{"lion", 16},
		{"leao", 16},
		{"Leão", 16},
		{"1", 1},
		{"01", 1},
		{"25", 25},
		{"17", 17},
	}

	for _, tt := range tests {
		animal, ok := FindAnimal(tt.query)
		if !ok || animal.Number != tt.expected {
			t.Errorf("FindAnimal(%q) = (%d, %v), expected group %d",
				tt.query, animal.Number, ok, tt.expected)
		}
	}
}

func TestEvaluateBichoBet_Grupo(t *testing.T) {
	// 1º: 4528 (Carneiro 07), 2º: 0967 (Macaco 17), 3º: 8115 (Borboleta 04), 4º: 3499 (Vaca 25), 5º: 7254 (Gato 14)
	prizes := [5]int{4528, 967, 8115, 3499, 7254}

	// 1. Grupo Carneiro (07) na cabeça -> WON
	betCarneiroCabeca := &database.DBBichoBet{
		BetType: "grupo",
		Scope:   "cabeca",
		Target:  "carneiro",
		Amount:  100,
	}
	res1 := EvaluateBichoBet(betCarneiroCabeca, prizes)
	if !res1.Won || res1.Payout != 1800 {
		t.Errorf("Expected win 1800 for Carneiro on head, got won=%v, payout=%d", res1.Won, res1.Payout)
	}

	// 2. Grupo Macaco (17) na cabeça -> LOST (came in 2nd, not 1st)
	betMacacoCabeca := &database.DBBichoBet{
		BetType: "grupo",
		Scope:   "cabeca",
		Target:  "macaco",
		Amount:  100,
	}
	res2 := EvaluateBichoBet(betMacacoCabeca, prizes)
	if res2.Won {
		t.Errorf("Expected loss for Macaco on head")
	}

	// 3. Grupo Macaco (17) cercado -> WON (3.6x)
	betMacacoCercado := &database.DBBichoBet{
		BetType: "grupo",
		Scope:   "cercado",
		Target:  "macaco",
		Amount:  100,
	}
	res3 := EvaluateBichoBet(betMacacoCercado, prizes)
	if !res3.Won || res3.Payout != 360 {
		t.Errorf("Expected win 360 for Macaco cercado, got won=%v, payout=%d", res3.Won, res3.Payout)
	}
}

func TestEvaluateBichoBet_DezenaCentenaMilhar(t *testing.T) {
	prizes := [5]int{4528, 967, 8115, 3499, 7254}

	// Dezena 28 na cabeça -> 60x -> 100 * 60 = 6000
	betDezena := &database.DBBichoBet{
		BetType: "dezena",
		Scope:   "cabeca",
		Target:  "28",
		Amount:  100,
	}
	resDezena := EvaluateBichoBet(betDezena, prizes)
	if !resDezena.Won || resDezena.Payout != 6000 {
		t.Errorf("Dezena head: expected 6000, got %d", resDezena.Payout)
	}

	// Centena 528 na cabeça -> 600x -> 10 * 600 = 6000
	betCentena := &database.DBBichoBet{
		BetType: "centena",
		Scope:   "cabeca",
		Target:  "528",
		Amount:  10,
	}
	resCentena := EvaluateBichoBet(betCentena, prizes)
	if !resCentena.Won || resCentena.Payout != 6000 {
		t.Errorf("Centena head: expected 6000, got %d", resCentena.Payout)
	}

	// Milhar 4528 na cabeça -> 4000x -> 10 * 4000 = 40000
	betMilhar := &database.DBBichoBet{
		BetType: "milhar",
		Scope:   "cabeca",
		Target:  "4528",
		Amount:  10,
	}
	resMilhar := EvaluateBichoBet(betMilhar, prizes)
	if !resMilhar.Won || resMilhar.Payout != 40000 {
		t.Errorf("Milhar head: expected 40000, got %d", resMilhar.Payout)
	}
}

func TestEvaluateBichoBet_DuqueTerno(t *testing.T) {
	// Prizes: 07 Carneiro, 17 Macaco, 04 Borboleta, 25 Vaca, 14 Gato
	prizes := [5]int{4528, 967, 8115, 3499, 7254}

	// Duque Carneiro & Macaco -> both present -> 18.5x
	betDuque := &database.DBBichoBet{
		BetType: "duque",
		Target:  "carneiro,macaco",
		Amount:  100,
	}
	resDuque := EvaluateBichoBet(betDuque, prizes)
	if !resDuque.Won || resDuque.Payout != 1850 {
		t.Errorf("Duque: expected 1850, got %d", resDuque.Payout)
	}

	// Duque Carneiro & Leão -> Leão not in prizes -> Lost
	betDuqueLost := &database.DBBichoBet{
		BetType: "duque",
		Target:  "carneiro,leao",
		Amount:  100,
	}
	if res := EvaluateBichoBet(betDuqueLost, prizes); res.Won {
		t.Errorf("Expected Duque loss when one animal is missing")
	}

	// Terno Carneiro, Macaco & Vaca -> all 3 present -> 130x
	betTerno := &database.DBBichoBet{
		BetType: "terno",
		Target:  "carneiro,macaco,vaca",
		Amount:  10,
	}
	resTerno := EvaluateBichoBet(betTerno, prizes)
	if !resTerno.Won || resTerno.Payout != 1300 {
		t.Errorf("Terno: expected 1300, got %d", resTerno.Payout)
	}
}

func TestGenerateBichoDraw(t *testing.T) {
	for i := 0; i < 50; i++ {
		prizes := GenerateBichoDraw()
		for j, p := range prizes {
			if p < 0 || p > 9999 {
				t.Fatalf("Prize %d out of bounds: %d", j, p)
			}
		}
	}
}

func TestCalculateNextDrawTime(t *testing.T) {
	next := CalculateNextDrawTime(20, 0)
	now := time.Now()
	if !next.After(now) {
		t.Errorf("Expected next draw time to be in the future, got %v", next)
	}
	if next.Hour() != 20 || next.Minute() != 0 {
		t.Errorf("Expected 20:00, got %02d:%02d", next.Hour(), next.Minute())
	}
}

func TestValidateAndFormatBichoBet(t *testing.T) {
	// 1. Group by English name and head
	v1, err := ValidateAndFormatBichoBet("group", "monkey", "head")
	if err != nil || v1.Modality != "group" || v1.Target != "Monkey" || v1.Scope != "head" {
		t.Errorf("Failed to validate group by English name: %v, %+v", err, v1)
	}

	// 2. Group by Portuguese name and Portuguese scope
	v2, err := ValidateAndFormatBichoBet("grupo", "macaco", "cabeca")
	if err != nil || v2.Modality != "group" || v2.Target != "Monkey" || v2.Scope != "head" {
		t.Errorf("Failed to validate group by Portuguese name: %v, %+v", err, v2)
	}

	// 3. Group by number and board
	v3, err := ValidateAndFormatBichoBet("g", "17", "board")
	if err != nil || v3.Modality != "group" || v3.Target != "Monkey" || v3.Scope != "board" {
		t.Errorf("Failed to validate group by number: %v, %+v", err, v3)
	}

	// 4. Tens
	v4, err := ValidateAndFormatBichoBet("tens", "28", "head")
	if err != nil || v4.Modality != "tens" || v4.Target != "28" || v4.Scope != "head" {
		t.Errorf("Failed to validate tens: %v, %+v", err, v4)
	}

	// 5. Hundreds
	v5, err := ValidateAndFormatBichoBet("hundreds", "528", "")
	if err != nil || v5.Modality != "hundreds" || v5.Target != "528" || v5.Scope != "head" {
		t.Errorf("Failed to validate hundreds: %v, %+v", err, v5)
	}

	// 6. Thousands
	v6, err := ValidateAndFormatBichoBet("thousands", "4528", "board")
	if err != nil || v6.Modality != "thousands" || v6.Target != "4528" || v6.Scope != "board" {
		t.Errorf("Failed to validate thousands: %v, %+v", err, v6)
	}

	// 7. Animal Pair
	v7, err := ValidateAndFormatBichoBet("pair", "monkey, lion", "")
	if err != nil || v7.Modality != "pair" || v7.Target != "Monkey,Lion" || v7.Scope != "board" {
		t.Errorf("Failed to validate pair: %v, %+v", err, v7)
	}

	// 8. Animal Trio
	v8, err := ValidateAndFormatBichoBet("trio", "1 17 25", "")
	if err != nil || v8.Modality != "trio" || v8.Target != "Ostrich,Monkey,Cow" || v8.Scope != "board" {
		t.Errorf("Failed to validate trio: %v, %+v", err, v8)
	}

	// 9. Invalid modality
	if _, err := ValidateAndFormatBichoBet("quadra", "1 2 3 4", ""); err == nil {
		t.Errorf("Expected error for invalid modality")
	}

	// 10. Invalid tens out of range
	if _, err := ValidateAndFormatBichoBet("tens", "105", ""); err == nil {
		t.Errorf("Expected error for tens > 99")
	}

	// 11. Pair with identical animals
	if _, err := ValidateAndFormatBichoBet("pair", "monkey monkey", ""); err == nil {
		t.Errorf("Expected error for duplicate animals in pair")
	}
}

