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

var tagPattern = regexp.MustCompile(`^[a-zA-Z0-9_()\-]+$`)
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
	return s.addAsset(ctx, id, "gelbooru", strconv.FormatInt(postID, 10), work, name, p.FileURL, fmt.Sprintf("https://gelbooru.com/index.php?page=post&s=view&id=%d", postID), p.Source)
}
func (s *Store) AddPortrait(ctx context.Context, id int64, c Character) (int64, error) {
	work := "original"
	if len(c.Works) > 0 {
		work = c.Works[0].Title
	}
	return s.addAsset(ctx, id, c.Provider, c.ExternalID, work, c.Name, c.Portrait, c.URL, "")
}
func (s *Store) addAsset(ctx context.Context, id int64, provider, externalID, work, name, fileURL, source, attribution string) (int64, error) {
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
	rel := fmt.Sprintf("%s/%d-%s/foto-%s-%s/%s-v1%s", slug(work), id, slug(name), slug(provider), slug(externalID), sum, ext)
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
	e = s.DB.QueryRowContext(ctx, `INSERT INTO gacha_assets(character_id,provider,external_id,source_url,attribution,sha256,path,media_type) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(character_id,sha256) DO UPDATE SET sha256=excluded.sha256 RETURNING id`, id, provider, externalID, source, attribution, sum, rel, mime).Scan(&assetID)
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
