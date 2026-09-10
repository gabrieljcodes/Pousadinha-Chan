package gacha

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Enabled                  bool
	MediaDir, PublicURL      string
	RollsPerHour, ClaimHours int
}

func LoadConfig() (Config, error) {
	c := Config{Enabled: os.Getenv("GACHA_ENABLED") == "true", MediaDir: os.Getenv("GACHA_MEDIA_DIR"), PublicURL: strings.TrimRight(os.Getenv("GACHA_PUBLIC_URL"), "/"), RollsPerHour: 10, ClaimHours: 3}
	if c.MediaDir == "" {
		c.MediaDir = "data/gacha"
	}
	for _, v := range []struct {
		key string
		dst *int
		max int
	}{{"GACHA_ROLLS_PER_HOUR", &c.RollsPerHour, 100}, {"GACHA_CLAIM_HOURS", &c.ClaimHours, 168}} {
		if raw := os.Getenv(v.key); raw != "" {
			n, e := strconv.Atoi(raw)
			if e != nil || n < 1 || n > v.max {
				return c, fmt.Errorf("invalid %s", v.key)
			}
			*v.dst = n
		}
	}
	if c.Enabled || c.PublicURL != "" {
		u, e := url.Parse(c.PublicURL)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
			return c, fmt.Errorf("GACHA_PUBLIC_URL must be an HTTPS origin, e.g. https://pousadinha.com")
		}
	}
	return c, nil
}
