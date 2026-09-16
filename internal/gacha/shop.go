package gacha

import (
	"bot/internal/locale"
	"context"
	"crypto/rand"
	"database/sql"
	"math/big"
	"time"
)

// ItemID represents a unique shop item identifier.
type ItemID string

const (
	ItemLootbox     ItemID = "lootbox"
	ItemRollReset   ItemID = "roll_reset"
	ItemClaimReset  ItemID = "claim_reset"
	ItemSnipeShield ItemID = "snipe_shield"
	ItemWishFlare   ItemID = "wish_flare"
	ItemGemBattery  ItemID = "gem_battery"
)

// ShopItem represents an item offered in the Astral Shop.
type ShopItem struct {
	ID         ItemID
	Price      int64
	Emoji      string
	Consumable bool
}

// Name returns the translated name of the item.
func (i ShopItem) Name() string {
	switch i.ID {
	case ItemLootbox:
		return locale.Text("gacha.item.lootbox.name")
	case ItemRollReset:
		return locale.Text("gacha.item.roll_reset.name")
	case ItemClaimReset:
		return locale.Text("gacha.item.claim_reset.name")
	case ItemSnipeShield:
		return locale.Text("gacha.item.snipe_shield.name")
	case ItemWishFlare:
		return locale.Text("gacha.item.wish_flare.name")
	case ItemGemBattery:
		return locale.Text("gacha.item.gem_battery.name")
	default:
		return string(i.ID)
	}
}

// Description returns the translated description of the item.
func (i ShopItem) Description() string {
	switch i.ID {
	case ItemLootbox:
		return locale.Text("gacha.item.lootbox.desc")
	case ItemRollReset:
		return locale.Text("gacha.item.roll_reset.desc")
	case ItemClaimReset:
		return locale.Text("gacha.item.claim_reset.desc")
	case ItemSnipeShield:
		return locale.Text("gacha.item.snipe_shield.desc")
	case ItemWishFlare:
		return locale.Text("gacha.item.wish_flare.desc")
	case ItemGemBattery:
		return locale.Text("gacha.item.gem_battery.desc")
	default:
		return ""
	}
}

var shopCatalog = []ShopItem{
	{
		ID:         ItemLootbox,
		Price:      5000,
		Emoji:      "📦",
		Consumable: true,
	},
	{
		ID:         ItemRollReset,
		Price:      2500,
		Emoji:      "⏳",
		Consumable: true,
	},
	{
		ID:         ItemClaimReset,
		Price:      8000,
		Emoji:      "💍",
		Consumable: true,
	},
	{
		ID:         ItemSnipeShield,
		Price:      10000,
		Emoji:      "🛡️",
		Consumable: true,
	},
	{
		ID:         ItemWishFlare,
		Price:      4000,
		Emoji:      "🌟",
		Consumable: true,
	},
	{
		ID:         ItemGemBattery,
		Price:      1000,
		Emoji:      "🔋",
		Consumable: true,
	},
}

// GetShopCatalog returns a copy of the available shop catalog items.
func (s *Store) GetShopCatalog() []ShopItem {
	items := make([]ShopItem, len(shopCatalog))
	copy(items, shopCatalog)
	return items
}

// FindShopItem returns the item by ID.
func (s *Store) FindShopItem(id ItemID) (ShopItem, bool) {
	for _, it := range shopCatalog {
		if it.ID == id {
			return it, true
		}
	}
	return ShopItem{}, false
}

// InventoryItem represents an owned item in the player's inventory.
type InventoryItem struct {
	Item     ShopItem
	Quantity int
}

// PlayerInventory represents the complete player bag and persistent perk upgrades.
type PlayerInventory struct {
	Balance             int64
	Items               []InventoryItem
	ExtraPermanentRolls int
	ExtraWishSlots      int
	PermanentWishBonus  float64
	StoredExtraRolls    int
	WishFlareRolls      int
	SnipeShieldUntil    time.Time
}

// BuyResult details a completed shop purchase.
type BuyResult struct {
	Item       ShopItem
	Quantity   int
	TotalCost  int64
	NewBalance int64
}

// UseResult details an item usage.
type UseResult struct {
	Item       ShopItem
	Message    string
	ExtraInfo  string
	LootReward *LootboxReward
}

// LootboxReward contains the random drop won from opening a Cosmic Chest.
type LootboxReward struct {
	Type        string // "perm_wish_bonus", "perm_roll", "wish_slot", "claim_reset", "roll_reset", "snipe_shield", "stored_rolls", "wish_flare"
	Title       string
	Description string
}

// BuyItem purchases items from the shop, debiting the player's server balance and updating inventory.
func (s *Store) BuyItem(ctx context.Context, guildID, userID string, itemID ItemID, quantity int) (*BuyResult, error) {
	if quantity < 1 || quantity > 100 {
		return nil, userError(locale.Text("gacha.shop.invalid_quantity"))
	}
	item, ok := s.FindShopItem(itemID)
	if !ok {
		return nil, userError(locale.Text("gacha.shop.invalid_item", locale.Data{"ID": string(itemID)}))
	}

	totalCost := item.Price * int64(quantity)

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if err = ensurePlayer(ctx, tx, guildID, userID); err != nil {
		return nil, err
	}

	var currentBalance int64
	err = tx.QueryRowContext(ctx, `SELECT balance FROM guild_members WHERE guild_id=$1 AND user_id=$2 FOR UPDATE`, guildID, userID).Scan(&currentBalance)
	if err != nil {
		return nil, err
	}

	if currentBalance < totalCost {
		return nil, userError(locale.Text("gacha.shop.insufficient_funds", locale.Data{
			"Cost":    totalCost,
			"Balance": currentBalance,
		}))
	}

	var newBalance int64
	err = tx.QueryRowContext(ctx, `
		UPDATE guild_members
		SET balance = balance - $1, updated_at = now()
		WHERE guild_id = $2 AND user_id = $3
		RETURNING balance
	`, totalCost, guildID, userID).Scan(&newBalance)
	if err != nil {
		return nil, err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO gacha_inventory (guild_id, user_id, item_id, quantity, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (guild_id, user_id, item_id)
		DO UPDATE SET quantity = gacha_inventory.quantity + excluded.quantity, updated_at = now()
	`, guildID, userID, string(item.ID), quantity)
	if err != nil {
		return nil, err
	}

	_, _ = tx.ExecContext(ctx, `
		INSERT INTO gacha_shop_logs (guild_id, user_id, action, item_id, cost, created_at)
		VALUES ($1, $2, 'buy', $3, $4, now())
	`, guildID, userID, string(item.ID), totalCost)

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return &BuyResult{
		Item:       item,
		Quantity:   quantity,
		TotalCost:  totalCost,
		NewBalance: newBalance,
	}, nil
}

// UseItem uses an item from inventory.
func (s *Store) UseItem(ctx context.Context, guildID, userID string, itemID ItemID) (*UseResult, error) {
	if itemID == ItemLootbox {
		reward, err := s.OpenLootbox(ctx, guildID, userID)
		if err != nil {
			return nil, err
		}
		item, _ := s.FindShopItem(itemID)
		return &UseResult{
			Item:       item,
			Message:    reward.Description,
			LootReward: reward,
		}, nil
	}

	item, ok := s.FindShopItem(itemID)
	if !ok {
		return nil, userError(locale.Text("gacha.shop.invalid_item", locale.Data{"ID": string(itemID)}))
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if err = ensurePlayer(ctx, tx, guildID, userID); err != nil {
		return nil, err
	}

	var qty int
	err = tx.QueryRowContext(ctx, `
		SELECT quantity FROM gacha_inventory
		WHERE guild_id=$1 AND user_id=$2 AND item_id=$3 FOR UPDATE
	`, guildID, userID, string(itemID)).Scan(&qty)
	if err == sql.ErrNoRows || qty <= 0 {
		return nil, userError(locale.Text("gacha.shop.item_not_in_inventory", locale.Data{"ItemName": item.Name()}))
	}
	if err != nil {
		return nil, err
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE gacha_inventory
		SET quantity = quantity - 1, updated_at = now()
		WHERE guild_id=$1 AND user_id=$2 AND item_id=$3
	`, guildID, userID, string(itemID))
	if err != nil {
		return nil, err
	}

	var resultMsg string

	switch itemID {
	case ItemRollReset:
		var rollsUsed int
		_ = tx.QueryRowContext(ctx, `SELECT rolls_used FROM gacha_players WHERE guild_id=$1 AND user_id=$2 FOR UPDATE`, guildID, userID).Scan(&rollsUsed)
		if rollsUsed == 0 {
			return nil, userError(locale.Text("gacha.use.roll_reset_already_full"))
		}
		_, err = tx.ExecContext(ctx, `UPDATE gacha_players SET rolls_used=0 WHERE guild_id=$1 AND user_id=$2`, guildID, userID)
		if err != nil {
			return nil, err
		}
		resultMsg = locale.Text("gacha.use.roll_reset_success")

	case ItemClaimReset:
		var claimAfter time.Time
		_ = tx.QueryRowContext(ctx, `SELECT claim_after FROM gacha_players WHERE guild_id=$1 AND user_id=$2 FOR UPDATE`, guildID, userID).Scan(&claimAfter)
		if !claimAfter.After(time.Now()) {
			return nil, userError(locale.Text("gacha.use.claim_reset_already_ready"))
		}
		_, err = tx.ExecContext(ctx, `UPDATE gacha_players SET claim_after=now() WHERE guild_id=$1 AND user_id=$2`, guildID, userID)
		if err != nil {
			return nil, err
		}
		resultMsg = locale.Text("gacha.use.claim_reset_success")

	case ItemSnipeShield:
		schedule := s.GuildSchedule(ctx, guildID)
		claimWin := schedule.ClaimWindow(time.Now())
		claimInterval := time.Duration(schedule.ClaimHours) * time.Hour
		if claimInterval <= 0 {
			claimInterval = 3 * time.Hour
		}

		var currentExpiry sql.NullTime
		_ = tx.QueryRowContext(ctx, `SELECT snipe_shield_until FROM gacha_players WHERE guild_id=$1 AND user_id=$2 FOR UPDATE`, guildID, userID).Scan(&currentExpiry)

		var newExpiry time.Time
		if currentExpiry.Valid && currentExpiry.Time.After(time.Now()) {
			// Extend existing active shield by 3 resets
			newExpiry = currentExpiry.Time.Add(3 * claimInterval)
		} else {
			// Current claim window ends at claimWin.NextReset (1st reset boundary).
			// 3 resets = claimWin.NextReset + 2 * claimInterval
			newExpiry = claimWin.NextReset.Add(2 * claimInterval)
		}

		_, err = tx.ExecContext(ctx, `UPDATE gacha_players SET snipe_shield_until=$3 WHERE guild_id=$1 AND user_id=$2`, guildID, userID, newExpiry)
		if err != nil {
			return nil, err
		}
		resultMsg = locale.Text("gacha.use.snipe_shield_success", locale.Data{"Unix": newExpiry.Unix()})

	case ItemWishFlare:
		var totalFlare int
		err = tx.QueryRowContext(ctx, `
			UPDATE gacha_players SET wish_flare_rolls = wish_flare_rolls + 10
			WHERE guild_id=$1 AND user_id=$2
			RETURNING wish_flare_rolls
		`, guildID, userID).Scan(&totalFlare)
		if err != nil {
			return nil, err
		}
		resultMsg = locale.Text("gacha.use.wish_flare_success", locale.Data{"Total": totalFlare})

	case ItemGemBattery:
		var currentPower int
		_ = tx.QueryRowContext(ctx, `SELECT gem_power FROM gacha_players WHERE guild_id=$1 AND user_id=$2 FOR UPDATE`, guildID, userID).Scan(&currentPower)
		if currentPower >= MaxGemPower {
			return nil, userError(locale.Text("gacha.use.gem_battery_already_full"))
		}
		_, err = tx.ExecContext(ctx, `UPDATE gacha_players SET gem_power=$3 WHERE guild_id=$1 AND user_id=$2`, guildID, userID, MaxGemPower)
		if err != nil {
			return nil, err
		}
		resultMsg = locale.Text("gacha.use.gem_battery_success")

	default:
		return nil, userError(locale.Text("gacha.shop.cannot_use_item"))
	}

	_, _ = tx.ExecContext(ctx, `
		INSERT INTO gacha_shop_logs (guild_id, user_id, action, item_id, created_at)
		VALUES ($1, $2, 'use', $3, now())
	`, guildID, userID, string(itemID))

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return &UseResult{
		Item:    item,
		Message: resultMsg,
	}, nil
}

// OpenLootbox opens a Cosmic Chest and grants a randomized drop.
func (s *Store) OpenLootbox(ctx context.Context, guildID, userID string) (*LootboxReward, error) {
	chestItem, _ := s.FindShopItem(ItemLootbox)

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if err = ensurePlayer(ctx, tx, guildID, userID); err != nil {
		return nil, err
	}

	var qty int
	err = tx.QueryRowContext(ctx, `
		SELECT quantity FROM gacha_inventory
		WHERE guild_id=$1 AND user_id=$2 AND item_id=$3 FOR UPDATE
	`, guildID, userID, string(ItemLootbox)).Scan(&qty)
	if err == sql.ErrNoRows || qty <= 0 {
		return nil, userError(locale.Text("gacha.shop.item_not_in_inventory", locale.Data{"ItemName": chestItem.Name()}))
	}
	if err != nil {
		return nil, err
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE gacha_inventory
		SET quantity = quantity - 1, updated_at = now()
		WHERE guild_id=$1 AND user_id=$2 AND item_id=$3
	`, guildID, userID, string(ItemLootbox))
	if err != nil {
		return nil, err
	}

	// Roll 0..9999
	rBig, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return nil, err
	}
	roll := rBig.Int64()

	var reward LootboxReward

	switch {
	case roll < 200: // 2%: Permanent +1% Wishlist Spawn Bonus
		var total float64
		err = tx.QueryRowContext(ctx, `
			UPDATE gacha_players
			SET permanent_wish_bonus = permanent_wish_bonus + 1.0
			WHERE guild_id=$1 AND user_id=$2
			RETURNING permanent_wish_bonus
		`, guildID, userID).Scan(&total)
		if err != nil {
			return nil, err
		}
		reward = LootboxReward{
			Type:        "perm_wish_bonus",
			Title:       locale.Text("gacha.lootbox.open_title"),
			Description: locale.Text("gacha.lootbox.reward_wish_bonus", locale.Data{"Total": total}),
		}

	case roll < 500: // 3%: Permanent +1 Roll/hour
		var total int
		err = tx.QueryRowContext(ctx, `
			UPDATE gacha_players
			SET extra_permanent_rolls = extra_permanent_rolls + 1
			WHERE guild_id=$1 AND user_id=$2
			RETURNING extra_permanent_rolls
		`, guildID, userID).Scan(&total)
		if err != nil {
			return nil, err
		}
		reward = LootboxReward{
			Type:        "perm_roll",
			Title:       locale.Text("gacha.lootbox.open_title"),
			Description: locale.Text("gacha.lootbox.reward_perm_roll", locale.Data{"Total": total}),
		}

	case roll < 1300: // 8%: Permanent +1 Wishlist Slot
		var total int
		err = tx.QueryRowContext(ctx, `
			UPDATE gacha_players
			SET extra_wish_slots = extra_wish_slots + 1
			WHERE guild_id=$1 AND user_id=$2
			RETURNING extra_wish_slots
		`, guildID, userID).Scan(&total)
		if err != nil {
			return nil, err
		}
		reward = LootboxReward{
			Type:        "wish_slot",
			Title:       locale.Text("gacha.lootbox.open_title"),
			Description: locale.Text("gacha.lootbox.reward_wish_slot", locale.Data{"Total": total}),
		}

	case roll < 2300: // 10%: Claim Reset (Bond Orb) in inventory
		_, err = tx.ExecContext(ctx, `
			INSERT INTO gacha_inventory (guild_id, user_id, item_id, quantity, updated_at)
			VALUES ($1, $2, $3, 1, now())
			ON CONFLICT (guild_id, user_id, item_id)
			DO UPDATE SET quantity = gacha_inventory.quantity + 1, updated_at = now()
		`, guildID, userID, string(ItemClaimReset))
		if err != nil {
			return nil, err
		}
		reward = LootboxReward{
			Type:        "claim_reset",
			Title:       locale.Text("gacha.lootbox.open_title"),
			Description: locale.Text("gacha.lootbox.reward_claim_reset"),
		}

	case roll < 3800: // 15%: Roll Reset (Time Hourglass) in inventory
		_, err = tx.ExecContext(ctx, `
			INSERT INTO gacha_inventory (guild_id, user_id, item_id, quantity, updated_at)
			VALUES ($1, $2, $3, 1, now())
			ON CONFLICT (guild_id, user_id, item_id)
			DO UPDATE SET quantity = gacha_inventory.quantity + 1, updated_at = now()
		`, guildID, userID, string(ItemRollReset))
		if err != nil {
			return nil, err
		}
		reward = LootboxReward{
			Type:        "roll_reset",
			Title:       locale.Text("gacha.lootbox.open_title"),
			Description: locale.Text("gacha.lootbox.reward_roll_reset"),
		}

	case roll < 5000: // 12%: Snipe Shield in inventory
		_, err = tx.ExecContext(ctx, `
			INSERT INTO gacha_inventory (guild_id, user_id, item_id, quantity, updated_at)
			VALUES ($1, $2, $3, 1, now())
			ON CONFLICT (guild_id, user_id, item_id)
			DO UPDATE SET quantity = gacha_inventory.quantity + 1, updated_at = now()
		`, guildID, userID, string(ItemSnipeShield))
		if err != nil {
			return nil, err
		}
		reward = LootboxReward{
			Type:        "snipe_shield",
			Title:       locale.Text("gacha.lootbox.open_title"),
			Description: locale.Text("gacha.lootbox.reward_snipe_shield"),
		}

	case roll < 7500: // 25%: +3 Stored Bonus Rolls
		var total int
		err = tx.QueryRowContext(ctx, `
			UPDATE gacha_players
			SET stored_extra_rolls = stored_extra_rolls + 3
			WHERE guild_id=$1 AND user_id=$2
			RETURNING stored_extra_rolls
		`, guildID, userID).Scan(&total)
		if err != nil {
			return nil, err
		}
		reward = LootboxReward{
			Type:        "stored_rolls",
			Title:       locale.Text("gacha.lootbox.open_title"),
			Description: locale.Text("gacha.lootbox.reward_stored_rolls", locale.Data{"Total": total}),
		}

	default: // 25%: Star Flare (+15% wish chance on next 10 rolls)
		var total int
		err = tx.QueryRowContext(ctx, `
			UPDATE gacha_players
			SET wish_flare_rolls = wish_flare_rolls + 10
			WHERE guild_id=$1 AND user_id=$2
			RETURNING wish_flare_rolls
		`, guildID, userID).Scan(&total)
		if err != nil {
			return nil, err
		}
		reward = LootboxReward{
			Type:        "wish_flare",
			Title:       locale.Text("gacha.lootbox.open_title"),
			Description: locale.Text("gacha.lootbox.reward_wish_flare"),
		}
	}

	_, _ = tx.ExecContext(ctx, `
		INSERT INTO gacha_shop_logs (guild_id, user_id, action, item_id, reward_type, reward_value, created_at)
		VALUES ($1, $2, 'lootbox_open', 'lootbox', $3, $4, now())
	`, guildID, userID, reward.Type, reward.Description)

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return &reward, nil
}

// GetPlayerInventory retrieves all items, coins, and active buffs for a user.
func (s *Store) GetPlayerInventory(ctx context.Context, guildID, userID string) (*PlayerInventory, error) {
	inv := &PlayerInventory{}

	// 1. Balance
	_ = s.DB.QueryRowContext(ctx, `
		SELECT balance FROM guild_members WHERE guild_id=$1 AND user_id=$2
	`, guildID, userID).Scan(&inv.Balance)

	// 2. Player perks
	var snipeUntil sql.NullTime
	_ = s.DB.QueryRowContext(ctx, `
		SELECT extra_permanent_rolls, extra_wish_slots, permanent_wish_bonus, stored_extra_rolls, wish_flare_rolls, snipe_shield_until
		FROM gacha_players
		WHERE guild_id=$1 AND user_id=$2
	`, guildID, userID).Scan(
		&inv.ExtraPermanentRolls,
		&inv.ExtraWishSlots,
		&inv.PermanentWishBonus,
		&inv.StoredExtraRolls,
		&inv.WishFlareRolls,
		&snipeUntil,
	)
	if snipeUntil.Valid {
		inv.SnipeShieldUntil = snipeUntil.Time
	}

	// 3. Items
	rows, err := s.DB.QueryContext(ctx, `
		SELECT item_id, quantity
		FROM gacha_inventory
		WHERE guild_id=$1 AND user_id=$2 AND quantity > 0
		ORDER BY item_id ASC
	`, guildID, userID)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var idStr string
			var q int
			if err := rows.Scan(&idStr, &q); err == nil {
				if it, ok := s.FindShopItem(ItemID(idStr)); ok {
					inv.Items = append(inv.Items, InventoryItem{Item: it, Quantity: q})
				}
			}
		}
	}

	return inv, nil
}
