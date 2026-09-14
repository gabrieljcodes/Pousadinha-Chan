package gacha

import (
	"bot/internal/mediastore"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Enabled                  bool
	Storage                  mediastore.Config
	MediaDir, PublicURL      string
	RollsPerHour, ClaimHours int
	WishBonusPercent         float64
	WishlistLimit            int
}

func LoadConfig() (Config, error) {
	c := Config{Enabled: os.Getenv("GACHA_ENABLED") == "true", MediaDir: os.Getenv("GACHA_MEDIA_DIR"), PublicURL: strings.TrimRight(os.Getenv("GACHA_PUBLIC_URL"), "/"), RollsPerHour: 10, ClaimHours: 3, WishBonusPercent: 2.0, WishlistLimit: 5}
	if c.MediaDir == "" {
		c.MediaDir = "data/gacha"
	}
	c.Storage = mediastore.Config{Backend: os.Getenv("GACHA_STORAGE_BACKEND"), Bucket: os.Getenv("GACHA_S3_BUCKET"), Region: os.Getenv("GACHA_S3_REGION"), Endpoint: os.Getenv("GACHA_S3_ENDPOINT"), Prefix: os.Getenv("GACHA_S3_PREFIX")}
	if raw := os.Getenv("GACHA_S3_PATH_STYLE"); raw != "" {
		var err error
		c.Storage.PathStyle, err = strconv.ParseBool(raw)
		if err != nil {
			return c, fmt.Errorf("GACHA_S3_PATH_STYLE must be a boolean")
		}
	}
	if err := c.Storage.Validate(); err != nil {
		return c, err
	}
	for _, v := range []struct {
		key string
		dst *int
		max int
	}{{"GACHA_ROLLS_PER_HOUR", &c.RollsPerHour, 100}, {"GACHA_CLAIM_HOURS", &c.ClaimHours, 168}, {"GACHA_WISHLIST_LIMIT", &c.WishlistLimit, 100}} {
		if raw := os.Getenv(v.key); raw != "" {
			n, e := strconv.Atoi(raw)
			if e != nil || n < 1 || n > v.max {
				return c, fmt.Errorf("invalid %s", v.key)
			}
			*v.dst = n
		}
	}
	if raw := os.Getenv("GACHA_WISH_BONUS_PERCENT"); raw != "" {
		p, err := strconv.ParseFloat(raw, 64)
		if err != nil || p < 0 || p > 100 {
			return c, fmt.Errorf("GACHA_WISH_BONUS_PERCENT must be a number between 0 and 100")
		}
		c.WishBonusPercent = p
	}
	if c.Enabled || c.PublicURL != "" {
		u, e := url.Parse(c.PublicURL)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
			return c, fmt.Errorf("GACHA_PUBLIC_URL must be an HTTPS origin, e.g. https://pousadinha.com")
		}
	}
	return c, nil
}
