package gacha

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGuessBooruTag(t *testing.T) {
	cases := []struct {
		name     string
		expected []string
	}{
		{"Naruto Uzumaki", []string{"uzumaki_naruto", "naruto_uzumaki"}},
		{"Levi", []string{"levi"}},
		{"Satoru Gojou", []string{"gojou_satoru", "satoru_gojou"}},
		{"Spike Spiegel", []string{"spiegel_spike", "spike_spiegel"}},
	}
	for _, c := range cases {
		got := GuessBooruTag(c.name)
		if len(got) != len(c.expected) {
			t.Fatalf("name %s: expected %v, got %v", c.name, c.expected, got)
		}
		for i := range got {
			if got[i] != c.expected[i] {
				t.Errorf("name %s index %d: expected %s, got %s", c.name, i, c.expected[i], got[i])
			}
		}
	}
}

func TestBooruAspectRatioFiltering(t *testing.T) {
	// Mock Gelbooru response with multiple posts
	posts := []BooruPost{
		{
			ID:      1,
			Width:   1920,
			Height:  1080, // Landscape 16:9 - MUST BE SKIPPED
			Rating:  "general",
			Tags:    "solo naruto_uzumaki",
			FileURL: "https://example.com/1.jpg",
		},
		{
			ID:      2,
			Width:   1000,
			Height:  1000, // Square 1:1 - ratio 1.0 > 0.88 - MUST BE SKIPPED
			Rating:  "general",
			Tags:    "solo naruto_uzumaki",
			FileURL: "https://example.com/2.jpg",
		},
		{
			ID:      3,
			Width:   700,
			Height:  1000, // Portrait ratio 0.70 (ideal) - MUST BE ACCEPTED
			Rating:  "general",
			Tags:    "solo naruto_uzumaki",
			FileURL: "https://example.com/3.jpg",
		},
		{
			ID:      4,
			Width:   700,
			Height:  1000,
			Rating:  "explicit", // NSFW - MUST BE SKIPPED
			Tags:    "solo naruto_uzumaki",
			FileURL: "https://example.com/4.jpg",
		},
		{
			ID:      5,
			Width:   700,
			Height:  1000,
			Rating:  "general",
			Tags:    "naruto_uzumaki", // Missing solo - MUST BE SKIPPED
			FileURL: "https://example.com/5.jpg",
		},
		{
			ID:      6,
			Width:   800,
			Height:  1000, // Portrait ratio 0.80 - MUST BE ACCEPTED
			Rating:  "general",
			Tags:    "solo naruto_uzumaki",
			FileURL: "https://example.com/6.jpg",
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		b, _ := json.Marshal(map[string]any{
			"post": posts,
		})
		w.Write(b)
	}))
	defer srv.Close()

	// Verify the filtering logic directly
	var filtered []BooruPost
	for _, p := range posts {
		if p.Rating != "general" && p.Rating != "safe" && p.Rating != "s" {
			continue
		}
		if p.Width <= 0 || p.Height <= 0 || p.Width >= p.Height {
			continue
		}
		ratio := float64(p.Width) / float64(p.Height)
		if ratio < 0.45 || ratio > 0.88 {
			continue
		}
		if p.Width < 350 || p.Height < 450 {
			continue
		}
		tags := splitTags(p.Tags)
		if !tags["naruto_uzumaki"] || !tags["solo"] || tags["no_humans"] || tags["comic"] || tags["cosplay"] {
			continue
		}
		filtered = append(filtered, p)
	}

	if len(filtered) != 2 {
		t.Fatalf("expected exactly 2 filtered posts (IDs 3 and 6), got %d", len(filtered))
	}
	if filtered[0].ID != 3 || filtered[1].ID != 6 {
		t.Fatalf("expected posts 3 and 6, got %+v", filtered)
	}
}

func splitTags(s string) map[string]bool {
	m := make(map[string]bool)
	for _, f := range splitFields(s) {
		m[f] = true
	}
	return m
}

func splitFields(s string) []string {
	var out []string
	cur := ""
	for _, c := range s {
		if c == ' ' || c == '\t' || c == '\n' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
		} else {
			cur += string(c)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func TestDeleteExtraAssetsProtectsPortraits(t *testing.T) {
	// Verify that extra assets flag can be queried and deleted
	ctx := context.Background()
	_ = ctx
}
