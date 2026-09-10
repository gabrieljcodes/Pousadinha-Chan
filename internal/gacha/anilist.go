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
		data, e := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		if e != nil {
			return e
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

const characterQuery = `query($id:Int!,$page:Int!){Character(id:$id){id name{full native alternative} gender description(asHtml:false) favourites siteUrl image{large} media(page:$page,perPage:25,sort:POPULARITY_DESC,type:ANIME){pageInfo{hasNextPage} edges{characterRole node{id type isAdult title{romaji english native} genres siteUrl studios(isMain:true){nodes{name}}}}}}}`

func (a *AniList) Character(ctx context.Context, id int64) (Character, error) {
	c := Character{Provider: "anilist", ExternalID: strconv.FormatInt(id, 10)}
	if id <= 0 {
		return c, fmt.Errorf("invalid AniList ID")
	}
	for page := 1; page <= 100; page++ {
		var result struct {
			Errors []struct{ Message string }
			Data   struct {
				Character *struct {
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
			}
		}
		if e := a.request(ctx, map[string]any{"query": characterQuery, "variables": map[string]any{"id": id, "page": page}}, &result); e != nil {
			return c, e
		}
		if len(result.Errors) > 0 {
			return c, fmt.Errorf("AniList GraphQL: %s", result.Errors[0].Message)
		}
		v := result.Data.Character
		if v == nil {
			return c, fmt.Errorf("character not found")
		}
		c.Name = v.Name.Full
		c.NativeName = v.Name.Native
		c.Aliases = v.Name.Alternative
		c.Gender = v.Gender
		c.Description = v.Description
		c.Favourites = v.Favourites
		c.URL = v.SiteURL
		c.Portrait = v.Image.Large
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
	}
	return c, fmt.Errorf("too many work pages; incomplete import refused")
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
