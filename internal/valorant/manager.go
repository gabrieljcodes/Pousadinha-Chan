package valorant

import (
    "estudocoin/internal/database"
    "estudocoin/pkg/config"
    "log"
    "time"
)

var valorantTicker *time.Ticker
var valorantStop chan bool

func StartValorantChecker() {
    interval := config.Economy.ValorantPollIntervalMinutes
    if interval <= 0 {
        interval = 5
    }
    valorantTicker = time.NewTicker(time.Duration(interval) * time.Minute)
    valorantStop = make(chan bool)

    go func() {
        for {
            select {
            case <-valorantTicker.C:
                CheckPendingBets()
            case <-valorantStop:
                return
            }
        }
    }()
}

func StopValorantChecker() {
    valorantTicker.Stop()
    close(valorantStop)
}

func CheckPendingBets() {
    bets, err := database.GetPendingValorantBets()
    if err != nil {
        log.Println("Error getting pending bets:", err)
        return
    }

    for _, bet := range bets {
        // Parse RiotID: name#tag
        parts := strings.Split(bet.RiotID, "#")
        if len(parts) != 2 {
            continue
        }
        name, tag := parts[0], parts[1]

        // Assuma region "br" (mude pra config)
        puuid, err := GetPUUID("br", name, tag)
        if err != nil {
            continue
        }

        matchIDs, err := GetRecentMatches("br", puuid)
        if err != nil || len(matchIDs) == 0 {
            continue
        }

        // Cheque o último match completo
        latestMatch, err := GetMatchDetails("br", matchIDs[0])
        if err != nil {
            continue
        }

        // Verifique se player perdeu ( !HasWon )
        lost := !latestMatch.HasWon
        wonBet := (bet.BetOnLoss && lost) || (!bet.BetOnLoss && !lost)

        // Resolva
        database.ResolveValorantBet(bet.ID, wonBet)
        // Envie notificação via webhook ou DM (use utils.SendWebhookNotification)
    }
}