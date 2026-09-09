package commands

import (
	"bot/internal/database"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{input: 0, want: "0"},
		{input: 7, want: "7"},
		{input: 99, want: "99"},
		{input: 500, want: "500"},
		{input: 1000, want: "1,000"},
		{input: 9999, want: "9,999"},
		{input: 10000, want: "10,000"},
		{input: 123456, want: "123,456"},
		{input: 1000000, want: "1,000,000"},
		{input: 50250750, want: "50,250,750"},
		{input: -50, want: "-50"},
		{input: -1250, want: "-1,250"},
		{input: -1000000, want: "-1,000,000"},
	}

	for _, tc := range tests {
		got := formatNumber(tc.input)
		if got != tc.want {
			t.Errorf("formatNumber(%d) = %q; want %q", tc.input, got, tc.want)
		}
	}
}

func TestGetMedal(t *testing.T) {
	tests := []struct {
		rank int
		want string
	}{
		{rank: 1, want: "🥇"},
		{rank: 2, want: "🥈"},
		{rank: 3, want: "🥉"},
		{rank: 4, want: "`#4`"},
		{rank: 5, want: "`#5`"},
		{rank: 10, want: "`#10`"},
		{rank: 99, want: "`#99`"},
	}

	for _, tc := range tests {
		got := getMedal(tc.rank)
		if got != tc.want {
			t.Errorf("getMedal(%d) = %q; want %q", tc.rank, got, tc.want)
		}
	}
}

func TestLeaderboardCacheLifecycle(t *testing.T) {
	testCat := "test_category"
	users := []database.UserBalance{
		{ID: "user_1", Balance: 5000, StockValue: 1000, CryptoValue: 500, TotalNetWorth: 6500},
	}
	streaks := []database.UserStreakRank{
		{ID: "user_1", Streak: 15, MaxStreak: 20},
	}

	// Clean state
	lbCacheMu.Lock()
	delete(lbCache, testCat)
	lbCacheMu.Unlock()

	// Initial check should miss
	_, _, found := getCachedLeaderboard(testCat)
	if found {
		t.Fatal("Expected cache miss before insertion")
	}

	// Insert into cache
	setCachedLeaderboard(testCat, users, streaks)

	// Now check should hit
	cachedUsers, cachedStreaks, found := getCachedLeaderboard(testCat)
	if !found {
		t.Fatal("Expected cache hit after insertion")
	}
	if len(cachedUsers) != 1 || cachedUsers[0].TotalNetWorth != 6500 {
		t.Errorf("Unexpected cached users: %+v", cachedUsers)
	}
	if len(cachedStreaks) != 1 || cachedStreaks[0].Streak != 15 {
		t.Errorf("Unexpected cached streaks: %+v", cachedStreaks)
	}

	// Manually age the cache entry beyond TTL
	lbCacheMu.Lock()
	lbCache[testCat] = leaderboardCacheEntry{
		timestamp: time.Now().Add(-2 * lbTTL),
		users:     users,
		streaks:   streaks,
	}
	lbCacheMu.Unlock()

	// Aged check should miss
	_, _, foundAfterExpiry := getCachedLeaderboard(testCat)
	if foundAfterExpiry {
		t.Fatal("Expected cache miss after entry expired")
	}
}

func TestResolveUsername(t *testing.T) {
	// 1. Nil session fallback to mention
	got1 := resolveUsername(nil, "guild_1", "123456789")
	if got1 != "<@123456789>" {
		t.Errorf("Expected mention fallback, got %q", got1)
	}

	// 2. Session with state but member not found
	session := &discordgo.Session{
		State: discordgo.NewState(),
	}
	got2 := resolveUsername(session, "guild_1", "987654321")
	if got2 != "<@987654321>" {
		t.Errorf("Expected mention fallback for uncached member, got %q", got2)
	}

	// 3. Cached member with Nick
	guild := &discordgo.Guild{
		ID: "guild_1",
		Members: []*discordgo.Member{
			{
				User: &discordgo.User{
					ID:       "user_with_nick",
					Username: "StandardUser",
				},
				Nick: "CoolNickname",
			},
		},
	}
	_ = session.State.GuildAdd(guild)

	got3 := resolveUsername(session, "guild_1", "user_with_nick")
	if got3 != "CoolNickname" {
		t.Errorf("Expected nickname 'CoolNickname', got %q", got3)
	}

	// 4. Cached member without Nick (uses Username)
	guild.Members = append(guild.Members, &discordgo.Member{
		User: &discordgo.User{
			ID:       "user_no_nick",
			Username: "PlainUsername",
		},
		Nick: "",
	})
	_ = session.State.GuildAdd(guild)

	got4 := resolveUsername(session, "guild_1", "user_no_nick")
	if got4 != "PlainUsername" {
		t.Errorf("Expected username 'PlainUsername', got %q", got4)
	}
}
