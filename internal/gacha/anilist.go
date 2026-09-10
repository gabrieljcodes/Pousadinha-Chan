package gacha

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type AniListHTTPError struct {
	Status  int
	Message string
}

func (e *AniListHTTPError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("AniList HTTP %d", e.Status)
	}
	return fmt.Sprintf("AniList HTTP %d: %s", e.Status, e.Message)
}

type AniList struct {
	Client   *http.Client
	Endpoint string
	mu       sync.Mutex
	next     time.Time
}

func NewAniList() *AniList {
	return &AniList{Client: &http.Client{Timeout: 25 * time.Second}, Endpoint: "https://graphql.anilist.co"}
}
func (a *AniList) request(ctx context.Context, payload any, out any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for attempt := 0; attempt < 4; attempt++ {
		timer := time.NewTimer(time.Until(a.next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		b, e := json.Marshal(payload)
		if e != nil {
			return e
		}
		req, e := http.NewRequestWithContext(ctx, "POST", a.Endpoint, bytes.NewReader(b))
		if e != nil {
			return e
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
		req.Header.Set("Referer", "https://anilist.co/")
		req.Header.Set("Origin", "https://anilist.co")
		resp, e := a.Client.Do(req)
		a.next = time.Now().Add(2200 * time.Millisecond)
		if e != nil {
			return fmt.Errorf("AniList request failed")
		}
		data, e := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
		resp.Body.Close()
		if e != nil {
			return e
		}
		if len(data) > 8<<20 {
			return fmt.Errorf("AniList response exceeds 8 MiB")
		}
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			delay := time.Duration(3*(attempt+1)) * time.Second
			if resp.StatusCode == 429 {
				delay = time.Minute
			}
			if date, err := http.ParseTime(resp.Header.Get("Retry-After")); err == nil && time.Until(date) > delay {
				delay = time.Until(date)
			}
			if n, e := strconv.Atoi(resp.Header.Get("Retry-After")); e == nil && n > 0 {
				delay = time.Duration(n) * time.Second
			}
			a.next = time.Now().Add(delay)
			continue
		}
		if resp.StatusCode != 200 {
			var body struct{ Errors []struct{ Message string } }
			message := ""
			if json.Unmarshal(data, &body) == nil && len(body.Errors) > 0 {
				message = clip(body.Errors[0].Message, 500)
			}
			return &AniListHTTPError{Status: resp.StatusCode, Message: message}
		}
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			pause := time.Now().Add(time.Minute)
			if reset, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil && time.Unix(reset, 0).After(pause) {
				pause = time.Unix(reset, 0)
			}
			a.next = pause
		}
		return json.Unmarshal(data, out)
	}
	return fmt.Errorf("AniList unavailable after retries")
}

const characterFields = `id name{full native alternative} gender description(asHtml:false) favourites siteUrl image{large} media(page:$mediaPage,perPage:25,sort:POPULARITY_DESC,type:ANIME){pageInfo{hasNextPage} edges{characterRole node{id type isAdult title{romaji english native} genres siteUrl studios(isMain:true){nodes{name}}}}}`
const characterQuery = `query($id:Int!,$mediaPage:Int!){Character(id:$id){` + characterFields + `}}`
const charactersQuery = `query($ids:[Int!]!,$mediaPage:Int!){Page(page:1,perPage:50){characters(id_in:$ids){` + characterFields + `}}}`

type anilistCharacter struct {
	ID   int64
	Name struct {
		Full, Native string
		Alternative  []string
	}
	Gender, Description, SiteURL string
	Image                        struct{ Large string }
	Favourites                   int
	Media                        struct {
		PageInfo struct{ HasNextPage bool }
		Edges    []struct {
			CharacterRole string
			Node          struct {
				ID      int64
				Type    string
				IsAdult bool
				Title   struct{ Romaji, English, Native string }
				Genres  []string
				SiteURL string
				Studios struct{ Nodes []struct{ Name string } }
			}
		}
	}
}

func (a *AniList) characterPage(ctx context.Context, id int64, page int) (*anilistCharacter, error) {
	var result struct {
		Errors []struct{ Message string }
		Data   struct{ Character *anilistCharacter }
	}
	if e := a.request(ctx, map[string]any{"query": characterQuery, "variables": map[string]any{"id": id, "mediaPage": page}}, &result); e != nil {
		return nil, e
	}
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("AniList GraphQL: %s", result.Errors[0].Message)
	}
	if result.Data.Character == nil {
		return nil, fmt.Errorf("character not found")
	}
	if result.Data.Character.ID != id {
		return nil, fmt.Errorf("AniList returned a different character")
	}
	return result.Data.Character, nil
}
func (a *AniList) completeCharacter(ctx context.Context, v *anilistCharacter) (Character, error) {
	c := Character{Provider: "anilist", ExternalID: strconv.FormatInt(v.ID, 10), Name: v.Name.Full, NativeName: v.Name.Native, Aliases: v.Name.Alternative, Gender: v.Gender, Description: v.Description, Favourites: v.Favourites, URL: v.SiteURL, Portrait: v.Image.Large}
	for page := 1; page <= 100; page++ {
		for _, edge := range v.Media.Edges {
			n := edge.Node
			if n.IsAdult {
				continue
			}
			title := n.Title.Romaji
			if title == "" {
				title = n.Title.English
			}
			w := Work{ExternalID: strconv.FormatInt(n.ID, 10), Kind: strings.ToLower(n.Type), Title: title, NativeTitle: n.Title.Native, Genres: n.Genres, URL: n.SiteURL, Role: edge.CharacterRole}
			for _, studio := range n.Studios.Nodes {
				w.Studios = append(w.Studios, studio.Name)
			}
			c.Works = append(c.Works, w)
		}
		if !v.Media.PageInfo.HasNextPage {
			return c, nil
		}
		if page == 100 {
			break
		}
		var e error
		v, e = a.characterPage(ctx, v.ID, page+1)
		if e != nil {
			return c, e
		}
	}
	return c, fmt.Errorf("too many work pages; incomplete import refused")
}
func (a *AniList) Character(ctx context.Context, id int64) (Character, error) {
	if id <= 0 {
		return Character{}, fmt.Errorf("invalid AniList ID")
	}
	v, e := a.characterPage(ctx, id, 1)
	if e != nil {
		return Character{}, e
	}
	return a.completeCharacter(ctx, v)
}

// Characters fetches up to 50 first pages together; only overflowing media connections
// need follow-up requests. GraphQL errors reject the whole response, never partial metadata.
func (a *AniList) Characters(ctx context.Context, ids []int64) ([]Character, error) {
	if len(ids) == 0 || len(ids) > 50 {
		return nil, fmt.Errorf("expected 1..50 character IDs")
	}
	requested := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 {
			return nil, fmt.Errorf("invalid AniList ID")
		}
		requested[id] = true
	}
	var result struct {
		Errors []struct{ Message string }
		Data   struct {
			Page *struct{ Characters []*anilistCharacter }
		}
	}
	if e := a.request(ctx, map[string]any{"query": charactersQuery, "variables": map[string]any{"ids": ids, "mediaPage": 1}}, &result); e != nil {
		return nil, e
	}
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("AniList GraphQL: %s", result.Errors[0].Message)
	}
	if result.Data.Page == nil {
		return nil, fmt.Errorf("AniList returned no page")
	}
	out := make([]Character, 0, len(ids))
	seen := map[int64]bool{}
	for _, v := range result.Data.Page.Characters {
		if v == nil || !requested[v.ID] || seen[v.ID] {
			return nil, fmt.Errorf("invalid character in batch response")
		}
		seen[v.ID] = true
		c, e := a.completeCharacter(ctx, v)
		if e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, nil
}

// CharacterIDs discovers identities in popularity order without assuming consecutive IDs.
func (a *AniList) CharacterIDs(ctx context.Context, page int) ([]int64, bool, error) {
	if page < 1 {
		return nil, false, fmt.Errorf("invalid page")
	}
	var result struct {
		Errors []struct{ Message string }
		Data   struct {
			Page *struct {
				PageInfo   struct{ HasNextPage bool }
				Characters []struct{ ID int64 }
			}
		}
	}
	e := a.request(ctx, map[string]any{"query": `query($page:Int!){Page(page:$page,perPage:50){pageInfo{hasNextPage} characters(sort:[FAVOURITES_DESC,ID]){id}}}`, "variables": map[string]any{"page": page}}, &result)
	if e != nil {
		return nil, false, e
	}
	if len(result.Errors) > 0 {
		return nil, false, fmt.Errorf("AniList GraphQL: %s", result.Errors[0].Message)
	}
	if result.Data.Page == nil {
		return nil, false, fmt.Errorf("AniList returned no page")
	}
	ids := make([]int64, 0, len(result.Data.Page.Characters))
	for _, c := range result.Data.Page.Characters {
		if c.ID <= 0 {
			return nil, false, fmt.Errorf("invalid source identity")
		}
		ids = append(ids, c.ID)
	}
	if len(ids) == 0 && result.Data.Page.PageInfo.HasNextPage {
		return nil, false, fmt.Errorf("empty discovery page with hasNextPage")
	}
	return ids, result.Data.Page.PageInfo.HasNextPage, nil
}
