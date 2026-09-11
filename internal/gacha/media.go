package gacha

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var tagPattern = regexp.MustCompile(`^[a-zA-Z0-9_()\-.]+$`)
var slugPattern = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = strings.Trim(slugPattern.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 70 {
		s = s[:70]
	}
	if s == "" {
		return "personagem"
	}
	return s
}

// Resolve and pin public addresses at dial time, including every redirect.
func mediaClient() *http.Client {
	tr := &http.Transport{TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(addr)
		if e != nil {
			return nil, e
		}
		if port != "443" || !(host == "s4.anilist.co" || host == "gelbooru.com" || strings.HasSuffix(host, ".gelbooru.com")) {
			return nil, fmt.Errorf("media host not allowed")
		}
		ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
		if e != nil {
			return nil, e
		}
		for _, v := range ips {
			if !v.IP.IsGlobalUnicast() || v.IP.IsPrivate() || v.IP.IsLoopback() || v.IP.IsLinkLocalUnicast() {
				return nil, fmt.Errorf("non-public media address")
			}
		}
		for _, v := range ips {
			conn, e := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(v.IP.String(), port))
			if e == nil {
				return conn, nil
			}
		}
		return nil, fmt.Errorf("media host unavailable")
	}
	return &http.Client{Transport: tr, Timeout: 45 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || req.URL.Scheme != "https" {
			return fmt.Errorf("redirect refused")
		}
		return nil
	}}
}

type BooruPost struct {
	ID      int64  `json:"id"`
	FileURL string `json:"file_url"`
	Rating  string `json:"rating"`
	Tags    string `json:"tags"`
	Source  string `json:"source"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Score   int    `json:"score"`
}

// GuessBooruTag derives candidate Gelbooru tags from character name.
func GuessBooruTag(name string) []string {
	clean := slugPattern.ReplaceAllString(strings.ToLower(name), " ")
	parts := strings.Fields(clean)
	if len(parts) == 0 {
		return nil
	}
	if len(parts) == 1 {
		return []string{parts[0]}
	}
	var candidates []string
	seen := make(map[string]bool)
	add := func(c string) {
		if c != "" && !seen[c] {
			seen[c] = true
			candidates = append(candidates, c)
		}
	}

	// 1. Japanese convention: surname_givenname (e.g. uzumaki_naruto, gojou_satoru)
	lastFirst := parts[len(parts)-1] + "_" + strings.Join(parts[:len(parts)-1], "_")
	add(lastFirst)

	// 2. Western convention: givenname_surname (e.g. edward_elric, spike_spiegel)
	firstLast := strings.Join(parts, "_")
	add(firstLast)

	// 3. Middle initial with dot (e.g. monkey_d._luffy)
	var dottedParts []string
	hasInit := false
	for _, p := range parts {
		if len(p) == 1 {
			dottedParts = append(dottedParts, p+".")
			hasInit = true
		} else {
			dottedParts = append(dottedParts, p)
		}
	}
	if hasInit {
		add(strings.Join(dottedParts, "_"))
	}

	// 4. Hepburn phonetic normalization (e.g. gojou -> gojo, shouto -> shoto, bakugou -> bakugo)
	if strings.Contains(lastFirst, "ou") {
		add(strings.ReplaceAll(lastFirst, "ou", "o"))
	}
	if strings.Contains(firstLast, "ou") {
		add(strings.ReplaceAll(firstLast, "ou", "o"))
	}
	if strings.Contains(lastFirst, "uu") {
		add(strings.ReplaceAll(lastFirst, "uu", "u"))
	}
	if strings.Contains(firstLast, "uu") {
		add(strings.ReplaceAll(firstLast, "uu", "u"))
	}

	return candidates
}

// FetchBooruTop fetches top-rated safe solo portrait posts from Gelbooru.
// Rejects landscape images (e.g. 1920x1080) and preserves character framing.
func FetchBooruTop(ctx context.Context, tag string, limit int) ([]BooruPost, error) {
	tag = strings.TrimSpace(tag)
	if !tagPattern.MatchString(tag) || limit <= 0 {
		return nil, fmt.Errorf("invalid tag or limit")
	}
	q := url.Values{
		"page":  {"dapi"},
		"s":     {"post"},
		"q":     {"index"},
		"json":  {"1"},
		"tags":  {tag + " rating:general solo sort:score -cosplay -no_humans -comic"},
		"limit": {"50"},
	}
	if key := os.Getenv("GELBOORU_API_KEY"); key != "" {
		q.Set("api_key", key)
		q.Set("user_id", os.Getenv("GELBOORU_USER_ID"))
	}
	req, e := http.NewRequestWithContext(ctx, "GET", "https://gelbooru.com/index.php?"+q.Encode(), nil)
	if e != nil {
		return nil, e
	}
	client := mediaClient()
	defer client.CloseIdleConnections()
	resp, e := client.Do(req)
	if e != nil {
		return nil, fmt.Errorf("Gelbooru request failed (check connectivity/credentials): %w", e)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Gelbooru HTTP %d", resp.StatusCode)
	}
	var body struct {
		Post []BooruPost `json:"post"`
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); e != nil {
		return nil, e
	}
	var out []BooruPost
	for _, p := range body.Post {
		if p.Rating != "general" && p.Rating != "safe" && p.Rating != "s" {
			continue
		}
		// Aspect ratio filter: skip landscape images (1920x1080) and squarish banners.
		// Card is 420x600 px (ratio ~0.70). Must be strictly portrait (height > width)
		// with width/height ratio between 0.45 and 0.88.
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
		postTags := strings.Fields(p.Tags)
		hasTag := false
		hasSolo := false
		hasBad := false
		for _, t := range postTags {
			if t == tag {
				hasTag = true
			}
			if t == "solo" {
				hasSolo = true
			}
			if t == "no_humans" || t == "comic" || t == "cosplay" {
				hasBad = true
			}
		}
		if !hasTag || !hasSolo || hasBad {
			continue
		}
		out = append(out, p)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}


func FetchBooru(ctx context.Context, tag string, postID int64) (BooruPost, error) {
	var p BooruPost
	if !tagPattern.MatchString(tag) || postID <= 0 {
		return p, fmt.Errorf("invalid tag or post ID")
	}
	q := url.Values{"page": {"dapi"}, "s": {"post"}, "q": {"index"}, "json": {"1"}, "id": {strconv.FormatInt(postID, 10)}, "tags": {tag + " rating:general"}, "limit": {"1"}}
	if key := os.Getenv("GELBOORU_API_KEY"); key != "" {
		q.Set("api_key", key)
		q.Set("user_id", os.Getenv("GELBOORU_USER_ID"))
	}
	req, e := http.NewRequestWithContext(ctx, "GET", "https://gelbooru.com/index.php?"+q.Encode(), nil)
	if e != nil {
		return p, e
	}
	client := mediaClient()
	defer client.CloseIdleConnections()
	resp, e := client.Do(req)
	if e != nil {
		return p, fmt.Errorf("Gelbooru request failed (check connectivity/credentials)")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return p, fmt.Errorf("Gelbooru HTTP %d", resp.StatusCode)
	}
	var body struct {
		Post []BooruPost `json:"post"`
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); e != nil {
		return p, e
	}
	if len(body.Post) != 1 {
		return p, fmt.Errorf("post not found")
	}
	p = body.Post[0]
	found := false
	for _, t := range strings.Fields(p.Tags) {
		if t == tag {
			found = true
		}
	}
	if p.ID != postID || !found || (p.Rating != "general" && p.Rating != "safe" && p.Rating != "s") {
		return p, fmt.Errorf("post does not match character tag or safe rating")
	}
	return p, nil
}
func Render(ctx context.Context, input, output string, animated bool) error {
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	f, e := os.Open(input)
	if e != nil {
		return e
	}
	cfg, _, e := image.DecodeConfig(f)
	f.Close()
	if e != nil {
		return fmt.Errorf("unsupported image: %w", e)
	}
	if cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 24000000 {
		return fmt.Errorf("image dimensions exceed limit")
	}
	filter := "scale=400:580:force_original_aspect_ratio=decrease:flags=lanczos,pad=420:600:(ow-iw)/2:(oh-ih)/2:color=0x161923,setsar=1,drawbox=x=3:y=3:w=414:h=594:color=0xc5a66b:t=2,drawbox=x=7:y=7:w=406:h=586:color=0x514633:t=1"
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-threads", "1", "-filter_complex_threads", "1", "-i", input, "-an", "-sn", "-map_metadata", "-1"}
	if animated {
		args = append(args, "-t", "12", "-filter_complex", "[0:v]fps=12,"+filter+",split[a][b];[a]palettegen=max_colors=128[p];[b][p]paletteuse=dither=bayer", "-loop", "0")
	} else {
		args = append(args, "-vf", filter, "-frames:v", "1")
	}
	args = append(args, output)
	if e = exec.CommandContext(ctx, "ffmpeg", args...).Run(); e != nil {
		return fmt.Errorf("image processing failed: %w", e)
	}
	st, e := os.Stat(output)
	if e != nil {
		return e
	}
	if st.Size() > 8<<20 {
		return fmt.Errorf("render exceeds 8 MiB")
	}
	return nil
}
func (s *Store) AddBooruAsset(ctx context.Context, id, postID int64) (int64, error) {
	var tag, name, work string
	e := s.DB.QueryRowContext(ctx, `SELECT t.tag,c.name,COALESCE((SELECT w.title FROM gacha_character_works cw JOIN gacha_works w ON w.id=cw.work_id WHERE cw.character_id=c.id ORDER BY w.id LIMIT 1),'original') FROM gacha_characters c JOIN gacha_booru_tags t ON t.character_id=c.id AND t.provider='gelbooru' WHERE c.id=$1`, id).Scan(&tag, &name, &work)
	if e != nil {
		return 0, e
	}
	p, e := FetchBooru(ctx, tag, postID)
	if e != nil {
		return 0, e
	}
	return s.addAsset(ctx, id, "gelbooru", strconv.FormatInt(postID, 10), work, name, p.FileURL, fmt.Sprintf("https://gelbooru.com/index.php?page=post&s=view&id=%d", postID), p.Source, true)
}
func (s *Store) AddPortrait(ctx context.Context, id int64, c Character) (int64, error) {
	work := "original"
	if len(c.Works) > 0 {
		work = c.Works[0].Title
	}
	return s.addAsset(ctx, id, c.Provider, c.ExternalID, work, c.Name, c.Portrait, c.URL, "", false)
}
func (s *Store) addAsset(ctx context.Context, id int64, provider, externalID, work, name, fileURL, source, attribution string, isExtra bool) (int64, error) {
	var existing int64
	e := s.DB.QueryRowContext(ctx, `SELECT id FROM gacha_assets WHERE character_id=$1 AND provider=$2 AND external_id=$3`, id, provider, externalID).Scan(&existing)
	if e == nil {
		return existing, nil
	}
	if e != sql.ErrNoRows {
		return 0, e
	}
	u, e := url.Parse(fileURL)
	if e != nil || u.Scheme != "https" || u.User != nil {
		return 0, fmt.Errorf("invalid media URL")
	}
	client := mediaClient()
	defer client.CloseIdleConnections()
	req, e := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if e != nil {
		return 0, e
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	if strings.Contains(u.Host, "gelbooru.com") {
		req.Header.Set("Referer", "https://gelbooru.com/")
	} else if strings.Contains(u.Host, "anilist.co") {
		req.Header.Set("Referer", "https://anilist.co/")
	}
	resp, e := client.Do(req)
	if e != nil {
		return 0, fmt.Errorf("media download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("media HTTP %d", resp.StatusCode)
	}
	temp, e := os.MkdirTemp("", "gacha-media-")
	if e != nil {
		return 0, e
	}
	defer os.RemoveAll(temp)
	input := filepath.Join(temp, "input")
	f, e := os.Create(input)
	if e != nil {
		return 0, e
	}
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, (12<<20)+1))
	ce := f.Close()
	if e != nil {
		return 0, e
	}
	if ce != nil {
		return 0, ce
	}
	if n > 12<<20 {
		return 0, fmt.Errorf("download exceeds 12 MiB")
	}
	f, e = os.Open(input)
	if e != nil {
		return 0, e
	}
	_, format, e := image.DecodeConfig(f)
	f.Close()
	if e != nil {
		return 0, e
	}
	ext, mime := ".png", "image/png"
	if format == "gif" {
		ext, mime = ".gif", "image/gif"
	}
	output := filepath.Join(temp, "card"+ext)
	if e = Render(ctx, input, output, format == "gif"); e != nil {
		return 0, e
	}
	sum := hex.EncodeToString(h.Sum(nil))
	rel := fmt.Sprintf("%s/%d-%s/photo-%s-%s/%s-v1%s", slug(work), id, slug(name), slug(provider), slug(externalID), sum, ext)
	dest := filepath.Join(s.Config.MediaDir, filepath.FromSlash(rel))
	if e = os.MkdirAll(filepath.Dir(dest), 0750); e != nil {
		return 0, e
	}
	// Atomic publication in the destination filesystem. DB approval is required to serve it.
	src, e := os.Open(output)
	if e != nil {
		return 0, e
	}
	defer src.Close()
	dst, e := os.CreateTemp(filepath.Dir(dest), ".pending-")
	if e != nil {
		return 0, e
	}
	defer os.Remove(dst.Name())
	_, e = io.Copy(dst, src)
	closeErr := dst.Close()
	if e != nil {
		return 0, e
	}
	if closeErr != nil {
		return 0, closeErr
	}
	if e = os.Rename(dst.Name(), dest); e != nil {
		return 0, e
	}
	var assetID int64
	e = s.DB.QueryRowContext(ctx, `INSERT INTO gacha_assets(character_id,provider,external_id,source_url,attribution,sha256,path,media_type,is_extra) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(character_id,sha256) DO UPDATE SET sha256=excluded.sha256 RETURNING id`, id, provider, externalID, source, attribution, sum, rel, mime, isExtra).Scan(&assetID)
	return assetID, e
}
func (s *Store) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rel := strings.TrimPrefix(r.URL.Path, "/")
	if !filepath.IsLocal(rel) {
		http.NotFound(w, r)
		return
	}
	var mime string
	e := s.DB.QueryRowContext(ctx, `SELECT media_type FROM gacha_assets WHERE path=$1 AND status='approved'`, rel).Scan(&mime)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	// os.Root also blocks symlink escapes from the configured storage root.
	root, e := os.OpenRoot(s.Config.MediaDir)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	defer root.Close()
	f, e := root.Open(rel)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=300")
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}

// AddBooruTop downloads and processes the top safe solo portrait images for a character from Gelbooru.
// It skips landscape images (like 1920x1080) and flags all created assets with is_extra=true.
func (s *Store) AddBooruTop(ctx context.Context, id int64, tag string, limit int, autoApprove bool) ([]int64, string, error) {
	var name, aliasesJSON, work string
	e := s.DB.QueryRowContext(ctx, `SELECT c.name,COALESCE(c.aliases::text, '[]'),COALESCE((SELECT w.title FROM gacha_character_works cw JOIN gacha_works w ON w.id=cw.work_id WHERE cw.character_id=c.id ORDER BY w.id LIMIT 1),'original') FROM gacha_characters c WHERE c.id=$1`, id).Scan(&name, &aliasesJSON, &work)
	if e != nil {
		return nil, "", fmt.Errorf("character %d not found: %w", id, e)
	}
	usedTag := strings.TrimSpace(tag)
	var posts []BooruPost
	if usedTag != "" {
		posts, e = FetchBooruTop(ctx, usedTag, limit)
		if e != nil {
			return nil, "", e
		}
		_, _ = s.DB.ExecContext(ctx, `INSERT INTO gacha_booru_tags(character_id,provider,tag) VALUES($1,'gelbooru',$2) ON CONFLICT(character_id,provider) DO UPDATE SET tag=excluded.tag`, id, usedTag)
	} else {
		var regTag string
		if err := s.DB.QueryRowContext(ctx, `SELECT tag FROM gacha_booru_tags WHERE character_id=$1 AND provider='gelbooru'`, id).Scan(&regTag); err == nil && regTag != "" {
			usedTag = regTag
			posts, _ = FetchBooruTop(ctx, usedTag, limit)
		}
		if len(posts) == 0 {
			var allNames []string
			allNames = append(allNames, name)
			var aliases []string
			if json.Unmarshal([]byte(aliasesJSON), &aliases) == nil {
				allNames = append(allNames, aliases...)
			}
			for _, n := range allNames {
				candidates := GuessBooruTag(n)
				for _, cand := range candidates {
					p, err := FetchBooruTop(ctx, cand, limit)
					if err == nil && len(p) > 0 {
						posts = p
						usedTag = cand
						_, _ = s.DB.ExecContext(ctx, `INSERT INTO gacha_booru_tags(character_id,provider,tag) VALUES($1,'gelbooru',$2) ON CONFLICT(character_id,provider) DO UPDATE SET tag=excluded.tag`, id, usedTag)
						break
					}
				}
				if len(posts) > 0 {
					break
				}
			}
		}
	}
	if len(posts) == 0 {
		return nil, usedTag, fmt.Errorf("no matching safe solo portrait images found on Gelbooru (tag: %s)", usedTag)
	}
	var assetIDs []int64
	for _, p := range posts {
		aid, err := s.addAsset(ctx, id, "gelbooru", strconv.FormatInt(p.ID, 10), work, name, p.FileURL, fmt.Sprintf("https://gelbooru.com/index.php?page=post&s=view&id=%d", p.ID), p.Source, true)
		if err != nil {
			log.Printf("[booru-top] failed to process post %d: %v", p.ID, err)
			continue
		}
		if autoApprove {
			_, _ = s.DB.ExecContext(ctx, `UPDATE gacha_assets SET status='approved',reviewed_by='auto:booru-top',reviewed_at=now() WHERE id=$1`, aid)
		}
		assetIDs = append(assetIDs, aid)
	}
	return assetIDs, usedTag, nil
}

// DeleteExtraAssets removes all non-official (is_extra=true) assets from both disk and database.
// If characterID is > 0, it removes only that character's extra assets.
// Official AniList portraits (is_extra=false) are strictly protected and never touched.
func (s *Store) DeleteExtraAssets(ctx context.Context, characterID int64) (int, error) {
	query := `SELECT id, path FROM gacha_assets WHERE is_extra = true`
	var args []any
	if characterID > 0 {
		query += ` AND character_id = $1`
		args = append(args, characterID)
	}
	rows, e := s.DB.QueryContext(ctx, query, args...)
	if e != nil {
		return 0, e
	}
	defer rows.Close()
	var paths []string
	var count int
	for rows.Next() {
		var aid int64
		var relPath string
		if e = rows.Scan(&aid, &relPath); e != nil {
			return 0, e
		}
		paths = append(paths, relPath)
		count++
	}
	if e = rows.Err(); e != nil {
		return 0, e
	}
	if count == 0 {
		return 0, nil
	}
	for _, rel := range paths {
		filePath := filepath.Join(s.Config.MediaDir, filepath.FromSlash(rel))
		_ = os.Remove(filePath)
	}
	delQuery := `DELETE FROM gacha_assets WHERE is_extra = true`
	if characterID > 0 {
		delQuery += ` AND character_id = $1`
		_, e = s.DB.ExecContext(ctx, delQuery, characterID)
	} else {
		_, e = s.DB.ExecContext(ctx, delQuery)
	}
	return count, e
}

type AssetInfo struct {
	ID         int64
	Provider   string
	ExternalID string
	SourceURL  string
	Path       string
	Status     string
	IsExtra    bool
}

// ListAssets returns all assets (official portraits and extras) registered for a character.
func (s *Store) ListAssets(ctx context.Context, characterID int64) ([]AssetInfo, error) {
	rows, e := s.DB.QueryContext(ctx, `SELECT id, provider, external_id, source_url, path, status, is_extra FROM gacha_assets WHERE character_id=$1 ORDER BY id`, characterID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []AssetInfo
	for rows.Next() {
		var a AssetInfo
		if e = rows.Scan(&a.ID, &a.Provider, &a.ExternalID, &a.SourceURL, &a.Path, &a.Status, &a.IsExtra); e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type BooruMassOptions struct {
	LimitPerChar int
	MaxChars     int
	AutoApprove  bool
	SkipExisting bool
	MinFavs      int
	CharacterIDs []int64
}

type BooruMassSummary struct {
	Processed int
	Success   int
	Skipped   int
	Images    int
}

// RunBooruMass runs mass discovery and import of top Gelbooru portrait arts across characters.
// It uses the character's name to automatically derive candidate search tags.
func (s *Store) RunBooruMass(ctx context.Context, opt BooruMassOptions, out io.Writer) (BooruMassSummary, error) {
	if opt.LimitPerChar <= 0 {
		opt.LimitPerChar = 5
	}
	var characters []struct {
		ID         int64
		Name       string
		Favourites int
	}

	if len(opt.CharacterIDs) > 0 {
		for _, cid := range opt.CharacterIDs {
			var c struct {
				ID         int64
				Name       string
				Favourites int
			}
			err := s.DB.QueryRowContext(ctx, `SELECT id, name, favourites FROM gacha_characters WHERE id=$1`, cid).Scan(&c.ID, &c.Name, &c.Favourites)
			if err == nil {
				characters = append(characters, c)
			}
		}
	} else {
		query := `SELECT c.id, c.name, c.favourites FROM gacha_characters c WHERE c.enabled = true`
		var args []any
		argIdx := 1
		if opt.SkipExisting {
			query += ` AND NOT EXISTS (SELECT 1 FROM gacha_assets a WHERE a.character_id = c.id AND a.is_extra = true)`
		}
		if opt.MinFavs > 0 {
			query += fmt.Sprintf(` AND c.favourites >= $%d`, argIdx)
			args = append(args, opt.MinFavs)
			argIdx++
		}
		query += ` ORDER BY c.favourites DESC, c.id ASC`
		if opt.MaxChars > 0 {
			query += fmt.Sprintf(` LIMIT $%d`, argIdx)
			args = append(args, opt.MaxChars)
		}
		rows, err := s.DB.QueryContext(ctx, query, args...)
		if err != nil {
			return BooruMassSummary{}, err
		}
		defer rows.Close()
		for rows.Next() {
			var c struct {
				ID         int64
				Name       string
				Favourites int
			}
			if err = rows.Scan(&c.ID, &c.Name, &c.Favourites); err != nil {
				return BooruMassSummary{}, err
			}
			characters = append(characters, c)
		}
		if err = rows.Err(); err != nil {
			return BooruMassSummary{}, err
		}
	}

	total := len(characters)
	fmt.Fprintf(out, "Starting Gelbooru mass import for %d characters (limit: %d/char, auto-approve: %v, skip-existing: %v)...\n", total, opt.LimitPerChar, opt.AutoApprove, opt.SkipExisting)

	summary := BooruMassSummary{}
	for i, c := range characters {
		select {
		case <-ctx.Done():
			fmt.Fprintf(out, "\nOperation interrupted. Processed %d/%d characters (%d images imported).\n", summary.Processed, total, summary.Images)
			return summary, ctx.Err()
		default:
		}

		summary.Processed++
		aids, usedTag, err := s.AddBooruTop(ctx, c.ID, "", opt.LimitPerChar, opt.AutoApprove)
		if err != nil || len(aids) == 0 {
			summary.Skipped++
			errMsg := "no matching images"
			if err != nil {
				errMsg = err.Error()
			}
			fmt.Fprintf(out, "[%d/%d] #%d %s (♥ %d): 0 images (%s)\n", i+1, total, c.ID, c.Name, c.Favourites, errMsg)
		} else {
			summary.Success++
			summary.Images += len(aids)
			fmt.Fprintf(out, "[%d/%d] #%d %s (♥ %d): imported %d images (tag: %s)\n", i+1, total, c.ID, c.Name, c.Favourites, len(aids), usedTag)
		}

		if i+1 < total {
			timer := time.NewTimer(1200 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return summary, ctx.Err()
			case <-timer.C:
			}
		}
	}

	fmt.Fprintf(out, "\nFinished mass import! %d/%d characters had images imported (%d total new images).\n", summary.Success, total, summary.Images)
	return summary, nil
}


