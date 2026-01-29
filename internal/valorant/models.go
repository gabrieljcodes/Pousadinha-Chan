package valorant

import "time"

// ValorantBet representa uma aposta pendente
type ValorantBet struct {
    ID         int       // Auto-increment DB
    UserID     string    // Discord User ID
    RiotID     string    // "Player#TAG"
    BetOnLoss  bool      // True se apostou em perda (sua ideia inicial)
    Amount     int       // Quantia apostada
    CreatedAt  time.Time // Quando apostou
    CheckedAt  time.Time // Última verificação
    MatchID    string    // ID da partida monitorada (opcional, pra precisão)
    Resolved   bool      // Se já resolvida
}

// MatchResult simplificado da API
type MatchResult struct {
    MatchID   string
    HasWon    bool  // True se time do player ganhou
    RoundsWon int   // Rounds do time do player
}