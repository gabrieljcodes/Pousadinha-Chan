package gacha

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"
)

// SkillClassID represents a build class.
type SkillClassID string

const (
	ClassTrickster SkillClassID = "trickster"
	ClassOracle    SkillClassID = "oracle"
	ClassMerchant  SkillClassID = "merchant"
	ClassGuardian  SkillClassID = "guardian"
)

// ClassInfo holds metadata for a build class.
type ClassInfo struct {
	ID          SkillClassID
	Name        string
	Emoji       string
	Title       string
	Description string
}

// SkillDef defines an individual skill in a class tree.
type SkillDef struct {
	ID          string
	ClassID     SkillClassID
	Tier        int
	PointsCost  int
	Name        string
	Emoji       string
	Description string
	PrereqID    string
}

const (
	MaxBuildPoints = 15
	RespecCoinCost = 1000
)

var (
	ErrSkillNotFound     = errors.New("skill not found")
	ErrSkillAlreadyOwned = errors.New("skill already unlocked")
	ErrPrereqNotMet      = errors.New("prerequisite skill not unlocked")
	ErrNotEnoughPoints   = errors.New("not enough build points")
	ErrNotEnoughCoins    = errors.New("not enough coins for respec")
)

var classes = []ClassInfo{
	{
		ID:          ClassTrickster,
		Name:        "Trapaceiro",
		Emoji:       "🃏",
		Title:       "O Mestre dos Truques & Ilusões",
		Description: "Especializado em caos, roubo de moedas, enganar snipers e invocar Clones Ilusórios (Fake Wishes).",
	},
	{
		ID:          ClassOracle,
		Name:        "Oráculo",
		Emoji:       "🔮",
		Title:       "O Astrônomo do Destino Cósmico",
		Description: "Especializado em leitura das estrelas, aumento substancial de Wishlist, previsão e proteção de desejos.",
	},
	{
		ID:          ClassMerchant,
		Name:        "Mercador",
		Emoji:       "⚖️",
		Title:       "O Alquimista da Economia Astral",
		Description: "Especializado em geração de moedas, valorização de gemas, reciclagem de kakera em divórcios e descontos na Loja.",
	},
	{
		ID:          ClassGuardian,
		Name:        "Guardião",
		Emoji:       "🛡️",
		Title:       "O Caçador e Protetor Implacável",
		Description: "Especializado em escudos anti-snipe em todos os rolls, tempo estendido de rolagem, capacidade de rolls e punição de invasores.",
	},
}

var skillCatalog = []SkillDef{
	// Class: Trickster (🃏)
	{
		ID:          "trickster_t1_pocket",
		ClassID:     ClassTrickster,
		Tier:        1,
		PointsCost:  1,
		Name:        "Mãos Leves",
		Emoji:       "🤹",
		Description: "20% de chance ao rolar de encontrar uma bolsa com 30 a 90 moedas bônus.",
	},
	{
		ID:          "trickster_t2_stride",
		ClassID:     ClassTrickster,
		Tier:        2,
		PointsCost:  2,
		Name:        "Passo Ilusório",
		Emoji:       "👟",
		Description: "Reduz o tempo de recarga de casamento/claim em 10 minutos permanentemente.",
		PrereqID:    "trickster_t1_pocket",
	},
	{
		ID:          "trickster_t3_clone",
		ClassID:     ClassTrickster,
		Tier:        3,
		PointsCost:  3,
		Name:        "Clone Ilusório",
		Emoji:       "🎭",
		Description: "12% de chance ao rolar de invocar um Fake Wish. Se outro jogador tentar pegar, recebe o personagem real mascarado; se você clicar, avisa o truque e revela o real após 3s!",
		PrereqID:    "trickster_t2_stride",
	},
	{
		ID:          "trickster_t4_bamboozle",
		ClassID:     ClassTrickster,
		Tier:        4,
		PointsCost:  5,
		Name:        "O Grande Espetáculo",
		Emoji:       "🎪",
		Description: "Ao reivindicar qualquer personagem, 15% de chance de restaurar imediatamente 1 roll gasto ou encontrar uma gema surpresa.",
		PrereqID:    "trickster_t3_clone",
	},

	// Class: Oracle (🔮)
	{
		ID:          "oracle_t1_gaze",
		ClassID:     ClassOracle,
		Tier:        1,
		PointsCost:  1,
		Name:        "Olhar Estelar",
		Emoji:       "✨",
		Description: "Aumenta em +4% a chance base permanente de rolar personagens da sua Wishlist.",
	},
	{
		ID:          "oracle_t2_foresight",
		ClassID:     ClassOracle,
		Tier:        2,
		PointsCost:  2,
		Name:        "Sexto Sentido",
		Emoji:       "👁️",
		Description: "15% de chance de ganhar 1 roll extra imediato ao rolar um personagem com menos de 70 kakera.",
		PrereqID:    "oracle_t1_gaze",
	},
	{
		ID:          "oracle_t3_convergence",
		ClassID:     ClassOracle,
		Tier:        3,
		PointsCost:  3,
		Name:        "Alinhamento dos Astros",
		Emoji:       "🌌",
		Description: "A cada 10 rolagens consecutivas sem sair Wish, seu próximo roll ganha +20% de Wish Bonus acumulado.",
		PrereqID:    "oracle_t2_foresight",
	},
	{
		ID:          "oracle_t4_decree",
		ClassID:     ClassOracle,
		Tier:        4,
		PointsCost:  5,
		Name:        "Vontade do Destino",
		Emoji:       "📜",
		Description: "Seus wishes recebem 20s de Snipe Shield automático ao rolar, e seu claim em wish tem 20% de chance de não resetar seu cooldown.",
		PrereqID:    "oracle_t3_convergence",
	},

	// Class: Merchant (⚖️)
	{
		ID:          "merchant_t1_touch",
		ClassID:     ClassMerchant,
		Tier:        1,
		PointsCost:  1,
		Name:        "Toque Dourado",
		Emoji:       "🪙",
		Description: "Gemas coletadas concedem +35% de moedas, e todo casamento concluído rende +50 moedas de bônus.",
	},
	{
		ID:          "merchant_t2_exchange",
		ClassID:     ClassMerchant,
		Tier:        2,
		PointsCost:  2,
		Name:        "Transmutação de Almas",
		Emoji:       "⚗️",
		Description: "Ao divorciar um personagem, você recebe 50% do valor de kakera convertido diretamente em moedas.",
		PrereqID:    "merchant_t1_touch",
	},
	{
		ID:          "merchant_t3_crucible",
		ClassID:     ClassMerchant,
		Tier:        3,
		PointsCost:  3,
		Name:        "Forja Esmeralda",
		Emoji:       "💎",
		Description: "Aumenta a chance de gemas raras (Safira, Rubi, Diamante) em 25% e duplica o ganho de Gem Power.",
		PrereqID:    "merchant_t2_exchange",
	},
	{
		ID:          "merchant_t4_tycoon",
		ClassID:     ClassMerchant,
		Tier:        4,
		PointsCost:  5,
		Name:        "Monopólio Astral",
		Emoji:       "🏛️",
		Description: "Desconto permanente de 20% em todos os itens da Loja Astral.",
		PrereqID:    "merchant_t3_crucible",
	},

	// Class: Guardian (🛡️)
	{
		ID:          "guardian_t1_draw",
		ClassID:     ClassGuardian,
		Tier:        1,
		PointsCost:  1,
		Name:        "Reflexo Rápido",
		Emoji:       "⚡",
		Description: "Concede 15 segundos de proteção Snipe Shield em TODOS os seus rolls (não apenas wishes).",
	},
	{
		ID:          "guardian_t2_endurance",
		ClassID:     ClassGuardian,
		Tier:        2,
		PointsCost:  2,
		Name:        "Vigor do Caçador",
		Emoji:       "🏹",
		Description: "Aumenta permanentemente sua capacidade de rolls em +1 roll por hora.",
		PrereqID:    "guardian_t1_draw",
	},
	{
		ID:          "guardian_t3_tracker",
		ClassID:     ClassGuardian,
		Tier:        3,
		PointsCost:  3,
		Name:        "Rastreador Implacável",
		Emoji:       "🎯",
		Description: "Concede +2 slots permanentes extras na sua Wishlist sem gastar itens da loja.",
		PrereqID:    "guardian_t2_endurance",
	},
	{
		ID:          "guardian_t4_aegis",
		ClassID:     ClassGuardian,
		Tier:        4,
		PointsCost:  5,
		Name:        "Fortaleza Inabalável",
		Emoji:       "🏰",
		Description: "Seus rolls duram 75s no chat antes de expirar. Se alguém tentar dar snipe enquanto protegido, o agressor paga 100 moedas de multa para você!",
		PrereqID:    "guardian_t3_tracker",
	},
}

// PlayerBuildProfile contains a player's build points, unlocked skills, and milestone stats.
type PlayerBuildProfile struct {
	GuildID         string
	UserID          string
	TotalPoints     int
	SpentPoints     int
	AvailablePoints int
	UnlockedSkills  map[string]bool
	ClassTiers      map[SkillClassID]int
	LastRespecAt    *time.Time

	// Milestone statistics for information
	TotalRolls  int
	TotalClaims int
	MaxKey      int
	TotalKeys   int
	TomesBought int
}

// Skill cache for fast lookups in hot paths. Key: "guild:user:skill" -> bool
var playerSkillCache sync.Map

func playerSkillCacheKey(guildID, userID, skillID string) string {
	return fmt.Sprintf("%s:%s:%s", guildID, userID, skillID)
}

func invalidatePlayerSkillCache(guildID, userID string) {
	prefix := fmt.Sprintf("%s:%s:", guildID, userID)
	playerSkillCache.Range(func(key, value any) bool {
		if k, ok := key.(string); ok && len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			playerSkillCache.Delete(key)
		}
		return true
	})
}

// GetClasses returns all available build classes.
func (s *Store) GetClasses() []ClassInfo {
	res := make([]ClassInfo, len(classes))
	copy(res, classes)
	return res
}

// GetClassInfo finds class info by ID.
func (s *Store) GetClassInfo(id SkillClassID) (ClassInfo, bool) {
	for _, c := range classes {
		if c.ID == id {
			return c, true
		}
	}
	return ClassInfo{}, false
}

// GetSkillDefs returns all skills.
func (s *Store) GetSkillDefs() []SkillDef {
	res := make([]SkillDef, len(skillCatalog))
	copy(res, skillCatalog)
	return res
}

// GetSkillDef finds a skill definition by ID.
func (s *Store) GetSkillDef(id string) (SkillDef, bool) {
	for _, sk := range skillCatalog {
		if sk.ID == id {
			return sk, true
		}
	}
	return SkillDef{}, false
}

// GetClassSkills returns all skills belonging to a specific class, ordered by Tier.
func (s *Store) GetClassSkills(classID SkillClassID) []SkillDef {
	var list []SkillDef
	for _, sk := range skillCatalog {
		if sk.ClassID == classID {
			list = append(list, sk)
		}
	}
	return list
}

// HasPlayerSkill checks if a user has unlocked a specific skill.
func (s *Store) HasPlayerSkill(ctx context.Context, guildID, userID, skillID string) bool {
	if s == nil || s.DB == nil {
		return false
	}
	cacheKey := playerSkillCacheKey(guildID, userID, skillID)
	if val, ok := playerSkillCache.Load(cacheKey); ok {
		return val.(bool)
	}

	var exists bool
	err := s.DB.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM gacha_player_skills
			WHERE guild_id=$1 AND user_id=$2 AND skill_id=$3
		)
	`, guildID, userID, skillID).Scan(&exists)
	if err != nil {
		return false
	}

	playerSkillCache.Store(cacheKey, exists)
	return exists
}

// CalculateEarnedPoints computes the player's total earned build points based on milestones and purchases.
func (s *Store) CalculateEarnedPoints(ctx context.Context, guildID, userID string) (totalPoints int, rolls, claims, maxKey, totalKeys, tomes int, err error) {
	// 1. Total rolls made by user in this guild
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM gacha_rolls WHERE guild_id=$1 AND user_id=$2`, guildID, userID).Scan(&rolls)

	// Roll Milestones: 25 (+1), 100 (+1), 250 (+1), 500 (+1), 1000 (+1) => max 5
	rollPoints := 0
	if rolls >= 25 {
		rollPoints++
	}
	if rolls >= 100 {
		rollPoints++
	}
	if rolls >= 250 {
		rollPoints++
	}
	if rolls >= 500 {
		rollPoints++
	}
	if rolls >= 1000 {
		rollPoints++
	}

	// 2. Total claimed characters in harem
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM gacha_collection WHERE guild_id=$1 AND user_id=$2`, guildID, userID).Scan(&claims)

	// Claim Milestones: 5 (+1), 20 (+1), 50 (+1), 100 (+1), 200 (+1) => max 5
	claimPoints := 0
	if claims >= 5 {
		claimPoints++
	}
	if claims >= 20 {
		claimPoints++
	}
	if claims >= 50 {
		claimPoints++
	}
	if claims >= 100 {
		claimPoints++
	}
	if claims >= 200 {
		claimPoints++
	}

	// 3. Key affinity milestones
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(keys), 0), COALESCE(SUM(keys), 0) FROM gacha_affinity WHERE guild_id=$1 AND user_id=$2`, guildID, userID).Scan(&maxKey, &totalKeys)

	keyPoints := 0
	if maxKey >= 2 {
		keyPoints++
	}
	if maxKey >= 4 {
		keyPoints++
	}
	if totalKeys >= 10 {
		keyPoints++
	}

	// 4. Shop arcane tomes purchased
	_ = s.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM gacha_shop_logs
		WHERE guild_id=$1 AND user_id=$2 AND item_id='arcane_tome' AND action='buy'
	`, guildID, userID).Scan(&tomes)
	if tomes > 3 {
		tomes = 3
	}

	total := rollPoints + claimPoints + keyPoints + tomes
	if total > MaxBuildPoints {
		total = MaxBuildPoints
	}
	return total, rolls, claims, maxKey, totalKeys, tomes, nil
}

// SyncPlayerBuildPoints updates or initializes the player's build points in the database.
func (s *Store) SyncPlayerBuildPoints(ctx context.Context, guildID, userID string) (*PlayerBuildProfile, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if err = ensurePlayer(ctx, tx, guildID, userID); err != nil {
		return nil, err
	}

	earnedBP, rolls, claims, maxKey, totalKeys, tomes, err := s.CalculateEarnedPoints(ctx, guildID, userID)
	if err != nil {
		return nil, err
	}

	// Upsert gacha_player_builds
	_, err = tx.ExecContext(ctx, `
		INSERT INTO gacha_player_builds (guild_id, user_id, total_points, spent_points)
		VALUES ($1, $2, $3, 0)
		ON CONFLICT (guild_id, user_id) DO UPDATE
		SET total_points = GREATEST(gacha_player_builds.total_points, EXCLUDED.total_points),
		    updated_at = now()
	`, guildID, userID, earnedBP)
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return s.GetPlayerBuildProfile(ctx, guildID, userID, rolls, claims, maxKey, totalKeys, tomes)
}

// GetPlayerBuildProfile fetches a player's complete build state.
func (s *Store) GetPlayerBuildProfile(ctx context.Context, guildID, userID string, cachedStats ...int) (*PlayerBuildProfile, error) {
	var totalPoints, spentPoints int
	var lastRespec sql.NullTime

	err := s.DB.QueryRowContext(ctx, `
		SELECT total_points, spent_points, last_respec_at
		FROM gacha_player_builds
		WHERE guild_id=$1 AND user_id=$2
	`, guildID, userID).Scan(&totalPoints, &spentPoints, &lastRespec)

	if err == sql.ErrNoRows {
		// First time access: sync
		return s.SyncPlayerBuildPoints(ctx, guildID, userID)
	}
	if err != nil {
		return nil, err
	}

	// Load unlocked skills
	rows, err := s.DB.QueryContext(ctx, `
		SELECT skill_id, class_id, tier
		FROM gacha_player_skills
		WHERE guild_id=$1 AND user_id=$2
	`, guildID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	unlocked := make(map[string]bool)
	classTiers := make(map[SkillClassID]int)

	for rows.Next() {
		var skID, cID string
		var tier int
		if scanErr := rows.Scan(&skID, &cID, &tier); scanErr == nil {
			unlocked[skID] = true
			classID := SkillClassID(cID)
			if tier > classTiers[classID] {
				classTiers[classID] = tier
			}
		}
	}

	profile := &PlayerBuildProfile{
		GuildID:         guildID,
		UserID:          userID,
		TotalPoints:     totalPoints,
		SpentPoints:     spentPoints,
		AvailablePoints: totalPoints - spentPoints,
		UnlockedSkills:  unlocked,
		ClassTiers:      classTiers,
	}
	if lastRespec.Valid {
		profile.LastRespecAt = &lastRespec.Time
	}

	if len(cachedStats) >= 5 {
		profile.TotalRolls = cachedStats[0]
		profile.TotalClaims = cachedStats[1]
		profile.MaxKey = cachedStats[2]
		profile.TotalKeys = cachedStats[3]
		profile.TomesBought = cachedStats[4]
	} else {
		_, rolls, claims, maxKey, totalKeys, tomes, _ := s.CalculateEarnedPoints(ctx, guildID, userID)
		profile.TotalRolls = rolls
		profile.TotalClaims = claims
		profile.MaxKey = maxKey
		profile.TotalKeys = totalKeys
		profile.TomesBought = tomes
	}

	return profile, nil
}

// UnlockSkill learns a new skill for the player if prerequisites and points allow.
func (s *Store) UnlockSkill(ctx context.Context, guildID, userID, skillID string) (*PlayerBuildProfile, error) {
	skill, ok := s.GetSkillDef(skillID)
	if !ok {
		return nil, ErrSkillNotFound
	}

	profile, err := s.GetPlayerBuildProfile(ctx, guildID, userID)
	if err != nil {
		return nil, err
	}

	if profile.UnlockedSkills[skillID] {
		return nil, ErrSkillAlreadyOwned
	}

	if skill.PrereqID != "" && !profile.UnlockedSkills[skill.PrereqID] {
		return nil, ErrPrereqNotMet
	}

	if profile.AvailablePoints < skill.PointsCost {
		return nil, ErrNotEnoughPoints
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Insert skill
	_, err = tx.ExecContext(ctx, `
		INSERT INTO gacha_player_skills (guild_id, user_id, class_id, skill_id, tier, points_cost)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, guildID, userID, string(skill.ClassID), skill.ID, skill.Tier, skill.PointsCost)
	if err != nil {
		return nil, err
	}

	// Update spent points
	_, err = tx.ExecContext(ctx, `
		UPDATE gacha_player_builds
		SET spent_points = spent_points + $3, updated_at = now()
		WHERE guild_id=$1 AND user_id=$2
	`, guildID, userID, skill.PointsCost)
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	invalidatePlayerSkillCache(guildID, userID)
	return s.GetPlayerBuildProfile(ctx, guildID, userID)
}

// RespecBuild resets all spent points and removes all learned skills.
func (s *Store) RespecBuild(ctx context.Context, guildID, userID string) (*PlayerBuildProfile, error) {
	profile, err := s.GetPlayerBuildProfile(ctx, guildID, userID)
	if err != nil {
		return nil, err
	}

	// Check 24-hour free respec cooldown or coin cost
	isFree := false
	if profile.LastRespecAt == nil || time.Since(*profile.LastRespecAt) >= 24*time.Hour {
		isFree = true
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if !isFree {
		var balance int64
		err = tx.QueryRowContext(ctx, `SELECT balance FROM guild_members WHERE guild_id=$1 AND user_id=$2 FOR UPDATE`, guildID, userID).Scan(&balance)
		if err != nil || balance < RespecCoinCost {
			return nil, ErrNotEnoughCoins
		}

		_, err = tx.ExecContext(ctx, `UPDATE guild_members SET balance = balance - $3 WHERE guild_id=$1 AND user_id=$2`, guildID, userID, RespecCoinCost)
		if err != nil {
			return nil, err
		}
	}

	// Clear skills
	_, err = tx.ExecContext(ctx, `DELETE FROM gacha_player_skills WHERE guild_id=$1 AND user_id=$2`, guildID, userID)
	if err != nil {
		return nil, err
	}

	// Reset spent points
	_, err = tx.ExecContext(ctx, `
		UPDATE gacha_player_builds
		SET spent_points = 0, last_respec_at = now(), updated_at = now()
		WHERE guild_id=$1 AND user_id=$2
	`, guildID, userID)
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	invalidatePlayerSkillCache(guildID, userID)
	return s.GetPlayerBuildProfile(ctx, guildID, userID)
}
