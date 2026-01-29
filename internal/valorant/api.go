package valorant

import (
    "encoding/json"
    "fmt"
    "net/http"
    "os"
    "time"
)

const RiotBaseURL = "https://br.api.riotgames.com"  // %s = region (ex: "americas")

var apiKey = os.Getenv("RIOT_API_KEY")

// GetPUUID by RiotID (name#tag)
func GetPUUID(region, name, tag string) (string, error) {
    url := fmt.Sprintf("%s/riot/account/v1/accounts/by-riot-id/%s/%s?api_key=%s", RiotBaseURL, region, name, tag, apiKey)
    resp, err := http.Get(url)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    var data map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&data)
    return data["puuid"].(string), nil
}

// GetRecentMatches lista IDs de matches recentes
func GetRecentMatches(region, puuid string) ([]string, error) {
    url := fmt.Sprintf("%s/val/match/v1/matchlists/by-puuid/%s?api_key=%s", RiotBaseURL, region, puuid, apiKey)
    resp, err := http.Get(url)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var data struct {
        History []struct {
            MatchID string `json:"matchId"`
        } `json:"history"`
    }
    json.NewDecoder(resp.Body).Decode(&data)
    var ids []string
    for _, h := range data.History {
        ids = append(ids, h.MatchID)
    }
    return ids, nil
}

// GetMatchDetails pega detalhes de uma match
func GetMatchDetails(region, matchID string) (MatchResult, error) {
    url := fmt.Sprintf("%s/val/match/v1/matches/%s?api_key=%s", RiotBaseURL, region, matchID, apiKey)
    resp, err := http.Get(url)
    if err != nil {
        return MatchResult{}, err
    }
    defer resp.Body.Close()

    var data struct {
        Metadata struct {
            MatchID string `json:"matchId"`
        } `json:"metadata"`
        Players struct {
            AllPlayers []struct {
                PUUID string `json:"puuid"`
                Team  string `json:"team"`
            } `json:"allPlayers"`
        } `json:"players"`
        Rounds []struct {
            WinningTeam string `json:"winningTeam"`
        } `json:"roundResults"`
    }
    json.NewDecoder(resp.Body).Decode(&data)

    // Contar rounds (ex: se >6 rounds won, ganhou - simplificado pra 5v5)
    roundsWon := 0
    for _, r := range data.Rounds {
        if r.WinningTeam == "Blue" { // Assuma time do player
            roundsWon++ // Precisa mapear time do player via PUUID
        }
    }
    hasWon := roundsWon >= 13 // Best of 25, mas typical win condition
    return MatchResult{MatchID: data.Metadata.MatchID, HasWon: hasWon, RoundsWon: roundsWon}, nil
}