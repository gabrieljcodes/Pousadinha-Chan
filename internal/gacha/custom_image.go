package gacha

import (
	"bot/internal/locale"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
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
	"path/filepath"
	"strings"
	"time"
)

// CustomImageResult holds the response after submitting a custom photo or GIF.
type CustomImageResult struct {
	AssetID      int64
	CharacterID  int64
	Status       string // "approved" or "pending"
	AutoApproved bool
	Path         string
	URL          string
	MediaType    string
	TotalActive  int
}

// CustomAssetSummary represents a user custom image submission.
type CustomAssetSummary struct {
	ID            int64
	CharacterID   int64
	CharacterName string
	Path          string
	SourceURL     string
	MediaType     string
	Status        string
	SubmittedBy   string
	CreatedAt     time.Time
}

// customMediaClient dials any public HTTPS endpoint while strictly blocking SSRF (private/loopback/link-local/metadata IPs).
func customMediaClient() *http.Client {
	tr := &http.Transport{
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if port != "443" && port != "80" {
			return nil, fmt.Errorf("media port not allowed")
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, v := range ips {
			if !v.IP.IsGlobalUnicast() || v.IP.IsPrivate() || v.IP.IsLoopback() || v.IP.IsLinkLocalUnicast() {
				return nil, fmt.Errorf("non-public media address")
			}
		}
		for _, v := range ips {
			conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(v.IP.String(), port))
			if err == nil {
				return conn, nil
			}
		}
		return nil, fmt.Errorf("media host unavailable")
	}
	return &http.Client{
		Transport: tr,
		Timeout:   35 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 || (req.URL.Scheme != "https" && req.URL.Scheme != "http") {
				return fmt.Errorf("redirect refused")
			}
			return nil
		},
	}
}

// CountUserCustomImages counts how many active custom images a user currently has (max 20).
func (s *Store) CountUserCustomImages(ctx context.Context, userID string) (int, error) {
	var count int
	err := s.DB.QueryRowContext(ctx, `
		SELECT count(*) FROM gacha_assets 
		WHERE provider = 'user_custom' 
		  AND submitted_by = $1 
		  AND archived_at IS NULL 
		  AND status != 'rejected'
	`, userID).Scan(&count)
	return count, err
}

// ListUserCustomImages lists all custom images submitted by a user.
func (s *Store) ListUserCustomImages(ctx context.Context, userID string) ([]CustomAssetSummary, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT a.id, a.character_id, c.name, COALESCE(a.path, ''), COALESCE(a.source_url, ''), a.media_type, a.status, COALESCE(a.submitted_by, ''), a.created_at
		FROM gacha_assets a
		JOIN gacha_characters c ON c.id = a.character_id
		WHERE a.provider = 'user_custom' 
		  AND a.submitted_by = $1 
		  AND a.archived_at IS NULL 
		  AND a.status != 'rejected'
		ORDER BY a.created_at DESC
		LIMIT 25
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []CustomAssetSummary
	for rows.Next() {
		var item CustomAssetSummary
		if err := rows.Scan(&item.ID, &item.CharacterID, &item.CharacterName, &item.Path, &item.SourceURL, &item.MediaType, &item.Status, &item.SubmittedBy, &item.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

// SubmitCustomImage downloads/receives a custom image/GIF, validates user quota (<= 20), renders it,
// and saves it into gacha_assets.
func (s *Store) SubmitCustomImage(ctx context.Context, guildID, userID, authorTag string, charID int64, mediaURL string, body io.Reader, autoApprove bool) (*CustomImageResult, error) {
	if s.DB == nil {
		return nil, errors.New("database not available")
	}

	// 1. Check user quota (max 20 active custom images)
	activeCount, err := s.CountUserCustomImages(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to check custom quota: %w", err)
	}
	if activeCount >= 20 {
		return nil, userError(locale.Text("gacha.custom.quota_exceeded"))
	}

	// 2. Validate character
	var charName, workTitle string
	err = s.DB.QueryRowContext(ctx, `
		SELECT c.name, COALESCE((SELECT w.title FROM gacha_character_works cw JOIN gacha_works w ON w.id=cw.work_id WHERE cw.character_id=c.id ORDER BY CASE cw.role WHEN 'MAIN' THEN 0 ELSE 1 END, w.id LIMIT 1), 'Original')
		FROM gacha_characters c WHERE c.id = $1
	`, charID).Scan(&charName, &workTitle)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, userError(locale.Text("gacha.custom.character_not_found"))
		}
		return nil, err
	}

	// 3. Obtain data stream (from URL or io.Reader)
	temp, err := os.MkdirTemp("", "gacha-custom-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(temp)

	input := filepath.Join(temp, "input")
	f, err := os.Create(input)
	if err != nil {
		return nil, err
	}

	h := sha256.New()
	var reader io.Reader = body
	if reader == nil {
		trimmedURL := strings.TrimSpace(mediaURL)
		if trimmedURL == "" {
			_ = f.Close()
			return nil, userError(locale.Text("gacha.custom.missing_url_or_file"))
		}
		parsedURL, err := url.Parse(trimmedURL)
		if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
			_ = f.Close()
			return nil, userError(locale.Text("gacha.custom.invalid_url"))
		}

		client := customMediaClient()
		req, err := http.NewRequestWithContext(ctx, "GET", trimmedURL, nil)
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
		resp, err := client.Do(req)
		if err != nil {
			_ = f.Close()
			return nil, userError(locale.Text("gacha.custom.download_failed"))
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			_ = f.Close()
			return nil, userError(locale.Text("gacha.custom.download_http_error"))
		}
		reader = resp.Body
	}

	written, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(reader, (25<<20)+1))
	closeErr := f.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if written > 25<<20 {
		return nil, userError(locale.Text("gacha.custom.file_too_large"))
	}
	if written < 100 {
		return nil, userError(locale.Text("gacha.custom.file_too_small"))
	}

	// 4. Detect format
	f, err = os.Open(input)
	if err != nil {
		return nil, err
	}
	_, format, err := image.DecodeConfig(f)
	_ = f.Close()
	if err != nil {
		return nil, userError(locale.Text("gacha.custom.unsupported_format"))
	}

	isGif := (format == "gif")
	ext, mime := ".png", "image/png"
	if isGif {
		ext, mime = ".gif", "image/gif"
	}

	// 5. Render standard card
	output := filepath.Join(temp, "card"+ext)
	if err = Render(ctx, input, output, isGif); err != nil {
		return nil, fmt.Errorf("failed to process image: %w", err)
	}

	sum := hex.EncodeToString(h.Sum(nil))
	shortSum := sum
	if len(shortSum) > 16 {
		shortSum = shortSum[:16]
	}
	rel := fmt.Sprintf("%s/%d-%s/photo-custom-%s/%s-v1%s", slug(workTitle), charID, slug(charName), slug(userID), shortSum, ext)

	storage, err := s.MediaStorage()
	if err != nil {
		return nil, err
	}
	if err = storage.PutFile(ctx, rel, output, mime); err != nil {
		return nil, err
	}

	// 6. Persist to gacha_assets
	status := "pending"
	var reviewedBy sql.NullString
	var reviewedAt *time.Time
	now := time.Now()
	if autoApprove {
		status = "approved"
		reviewedBy = sql.NullString{String: "auto:guild-auto-approve", Valid: true}
		reviewedAt = &now
	}

	externalID := fmt.Sprintf("%s-%d", userID, time.Now().UnixNano())
	var assetID int64
	err = s.DB.QueryRowContext(ctx, `
		INSERT INTO gacha_assets(
			character_id, provider, external_id, source_url, attribution, sha256, path, media_type, is_extra, status, reviewed_by, reviewed_at, submitted_by, guild_id
		) VALUES (
			$1, 'user_custom', $2, $3, $4, $5, $6, $7, true, $8, $9, $10, $11, $12
		)
		ON CONFLICT (character_id, sha256) DO UPDATE SET
			path = EXCLUDED.path,
			media_type = EXCLUDED.media_type,
			status = EXCLUDED.status,
			reviewed_by = EXCLUDED.reviewed_by,
			reviewed_at = EXCLUDED.reviewed_at,
			submitted_by = EXCLUDED.submitted_by,
			guild_id = EXCLUDED.guild_id,
			archived_at = NULL
		RETURNING id
	`, charID, externalID, mediaURL, authorTag, sum, rel, mime, status, reviewedBy, reviewedAt, userID, guildID).Scan(&assetID)
	if err != nil {
		return nil, err
	}

	imageURL := rel
	if s.Config.PublicURL != "" {
		imageURL = s.Config.PublicURL + "/" + rel
	}

	return &CustomImageResult{
		AssetID:      assetID,
		CharacterID:  charID,
		Status:       status,
		AutoApproved: autoApprove,
		Path:         rel,
		URL:          imageURL,
		MediaType:    mime,
		TotalActive:  activeCount + 1,
	}, nil
}

// RemoveCustomImage deletes/rejects a custom image, freeing up the user's 20-slot quota.
func (s *Store) RemoveCustomImage(ctx context.Context, userID string, assetID int64, isAdmin bool) error {
	var charID int64
	var path string
	err := s.DB.QueryRowContext(ctx, `
		UPDATE gacha_assets 
		SET archived_at = now(), status = 'rejected'
		WHERE id = $1 AND (submitted_by = $2 OR $3 = true) AND archived_at IS NULL
		RETURNING character_id, COALESCE(path, '')
	`, assetID, userID, isAdmin).Scan(&charID, &path)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return userError(locale.Text("gacha.custom.asset_not_found_or_not_owner"))
		}
		return err
	}

	// Remove from guild active character image if present
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM gacha_guild_character_images WHERE asset_id = $1`, assetID)
	return nil
}

// ApproveCustomImage marks a pending asset as approved.
func (s *Store) ApproveCustomImage(ctx context.Context, reviewerID string, assetID int64) (*CustomAssetSummary, error) {
	var item CustomAssetSummary
	err := s.DB.QueryRowContext(ctx, `
		UPDATE gacha_assets 
		SET status = 'approved', reviewed_by = $2, reviewed_at = now()
		WHERE id = $1 AND status = 'pending' AND archived_at IS NULL
		RETURNING id, character_id, COALESCE(path, ''), COALESCE(source_url, ''), media_type, status, COALESCE(submitted_by, ''), created_at
	`, assetID, reviewerID).Scan(&item.ID, &item.CharacterID, &item.Path, &item.SourceURL, &item.MediaType, &item.Status, &item.SubmittedBy, &item.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, userError(locale.Text("gacha.custom.asset_not_pending"))
		}
		return nil, err
	}
	_ = s.DB.QueryRowContext(ctx, `SELECT name FROM gacha_characters WHERE id = $1`, item.CharacterID).Scan(&item.CharacterName)
	return &item, nil
}

// RejectCustomImage marks a pending asset as rejected.
func (s *Store) RejectCustomImage(ctx context.Context, reviewerID string, assetID int64) (*CustomAssetSummary, error) {
	var item CustomAssetSummary
	err := s.DB.QueryRowContext(ctx, `
		UPDATE gacha_assets 
		SET status = 'rejected', reviewed_by = $2, reviewed_at = now(), archived_at = now()
		WHERE id = $1 AND status = 'pending'
		RETURNING id, character_id, COALESCE(path, ''), COALESCE(source_url, ''), media_type, status, COALESCE(submitted_by, ''), created_at
	`, assetID, reviewerID).Scan(&item.ID, &item.CharacterID, &item.Path, &item.SourceURL, &item.MediaType, &item.Status, &item.SubmittedBy, &item.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, userError(locale.Text("gacha.custom.asset_not_pending"))
		}
		return nil, err
	}
	_ = s.DB.QueryRowContext(ctx, `SELECT name FROM gacha_characters WHERE id = $1`, item.CharacterID).Scan(&item.CharacterName)
	return &item, nil
}

// SetCharacterActiveImage sets which approved image is displayed for a character in the guild.
// If claimed, only the owner or an admin can set it. If unclaimed, anyone can set it.
func (s *Store) SetCharacterActiveImage(ctx context.Context, guildID, userID string, charID int64, imageIndex int, isAdmin bool) (*Card, error) {
	if imageIndex < 1 {
		return nil, userError(locale.Text("gacha.custom.invalid_image_index"))
	}

	// 1. Check ownership
	var ownerID sql.NullString
	_ = s.DB.QueryRowContext(ctx, `SELECT user_id FROM gacha_collection WHERE guild_id = $1 AND character_id = $2`, guildID, charID).Scan(&ownerID)
	if ownerID.Valid && ownerID.String != "" && ownerID.String != userID && !isAdmin {
		return nil, userError(locale.Text("gacha.custom.only_owner_can_set_image"))
	}

	// 2. Lookup asset at offset imageIndex-1 among approved assets
	var assetID int64
	var assetPath, sourceURL, attr sql.NullString
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, path, source_url, attribution 
		FROM gacha_assets 
		WHERE character_id = $1 AND status = 'approved' AND archived_at IS NULL
		ORDER BY id
		OFFSET $2 LIMIT 1
	`, charID, imageIndex-1).Scan(&assetID, &assetPath, &sourceURL, &attr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, userError(locale.Text("gacha.custom.image_index_not_found"))
		}
		return nil, err
	}

	// 3. Upsert into gacha_guild_character_images
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO gacha_guild_character_images(guild_id, character_id, asset_id, set_by, updated_at)
		VALUES($1, $2, $3, $4, now())
		ON CONFLICT (guild_id, character_id) DO UPDATE SET
			asset_id = EXCLUDED.asset_id,
			set_by = EXCLUDED.set_by,
			updated_at = now()
	`, guildID, charID, assetID, userID)
	if err != nil {
		return nil, err
	}

	// 4. Return updated Card
	c, _, err := s.FindCharacter(ctx, guildID, fmt.Sprint(charID))
	if err != nil {
		return nil, err
	}
	if assetPath.Valid && assetPath.String != "" {
		c.Image = assetPath.String
	}
	if sourceURL.Valid && sourceURL.String != "" {
		c.Source = sourceURL.String
	}
	if attr.Valid {
		c.Attribution = attr.String
	}
	return &c, nil
}
