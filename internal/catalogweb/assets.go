package catalogweb

import (
	"bot/internal/gacha"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"image"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (s *Server) assetFile(w http.ResponseWriter, r *http.Request) {
	id := pathID(w, r)
	if id == 0 {
		return
	}
	var path, mime, sourceURL sql.NullString
	if e := s.Store.DB.QueryRowContext(r.Context(), `SELECT path,media_type,source_url FROM gacha_assets WHERE id=$1`, id).Scan(&path, &mime, &sourceURL); e != nil {
		if e == sql.ErrNoRows {
			failure(w, 404, "Photo not found.")
		} else {
			internal(w, e)
		}
		return
	}
	if path.Valid && path.String != "" {
		s.Store.ServeMedia(w, r, path.String, mime.String, true)
		return
	}
	if sourceURL.Valid && (strings.HasPrefix(sourceURL.String, "http://") || strings.HasPrefix(sourceURL.String, "https://")) {
		http.Redirect(w, r, sourceURL.String, http.StatusFound)
		return
	}
	failure(w, 404, "The photo file is unavailable on this server.")
}

func (s *Server) assetAction(w http.ResponseWriter, r *http.Request) {
	id := pathID(w, r)
	if id == 0 {
		return
	}
	var in struct {
		Action      string `json:"action"`
		Value       bool   `json:"value"`
		Status      string `json:"status"`
		Attribution string `json:"attribution"`
	}
	if !decode(w, r, &in) {
		return
	}
	tx, e := s.Store.DB.BeginTx(r.Context(), nil)
	if e != nil {
		internal(w, e)
		return
	}
	defer tx.Rollback()
	var cid int64
	e = tx.QueryRowContext(r.Context(), `SELECT c.id FROM gacha_characters c JOIN gacha_assets a ON a.character_id=c.id WHERE a.id=$1 FOR UPDATE OF c`, id).Scan(&cid)
	if e == sql.ErrNoRows {
		failure(w, 404, "Photo not found.")
		return
	}
	if e != nil {
		internal(w, e)
		return
	}
	switch in.Action {
	case "favorite":
		_, e = tx.ExecContext(r.Context(), `UPDATE gacha_assets SET editorial_favorite=$2 WHERE id=$1`, id, in.Value)
	case "primary":
		var valid bool
		e = tx.QueryRowContext(r.Context(), `SELECT status='approved' AND archived_at IS NULL FROM gacha_assets WHERE id=$1`, id).Scan(&valid)
		if e == nil && !valid {
			failure(w, 409, "Approve and restore the photo before using it as the portrait.")
			return
		}
		if e == nil {
			_, e = tx.ExecContext(r.Context(), `UPDATE gacha_assets SET is_primary=false WHERE character_id=$1 AND is_primary`, cid)
		}
		if e == nil {
			_, e = tx.ExecContext(r.Context(), `UPDATE gacha_assets SET is_primary=true WHERE id=$1`, id)
		}
	case "archive":
		_, e = tx.ExecContext(r.Context(), `UPDATE gacha_assets SET archived_at=CASE WHEN $2 THEN now() ELSE NULL END,status=CASE WHEN $2 THEN 'rejected' ELSE 'pending' END,is_primary=false WHERE id=$1`, id, in.Value)
	case "review":
		if in.Status != "approved" && in.Status != "pending" && in.Status != "rejected" {
			failure(w, 400, "Invalid review status.")
			return
		}
		var result sql.Result
		result, e = tx.ExecContext(r.Context(), `UPDATE gacha_assets SET status=$2,reviewed_by='catalog:admin',reviewed_at=now(),is_primary=CASE WHEN $2='approved' THEN is_primary ELSE false END WHERE id=$1 AND archived_at IS NULL`, id, in.Status)
		if e == nil {
			n, _ := result.RowsAffected()
			if n == 0 {
				failure(w, 409, "Restore this photo before reviewing it.")
				return
			}
		}
	case "attribution":
		if len(in.Attribution) > 2000 {
			failure(w, 400, "Attribution must be at most 2,000 characters.")
			return
		}
		_, e = tx.ExecContext(r.Context(), `UPDATE gacha_assets SET attribution=$2 WHERE id=$1`, id, in.Attribution)
	default:
		failure(w, 400, "Unknown photo action.")
		return
	}
	if e == nil {
		_, e = tx.ExecContext(r.Context(), `UPDATE gacha_characters SET enabled=false,auto_publish_blocked=true WHERE id=$1 AND enabled AND NOT EXISTS(SELECT 1 FROM gacha_assets WHERE character_id=$1 AND status='approved')`, cid)
	}
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		internal(w, e)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	id := pathID(w, r)
	if id == 0 {
		return
	}
	select {
	case s.uploads <- struct{}{}:
		defer func() { <-s.uploads }()
	default:
		failure(w, 429, "Two photos are processing. Please try again shortly.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 13<<20)
	if e := r.ParseMultipartForm(1 << 20); e != nil {
		failure(w, 400, "Choose an image no larger than 12 MiB.")
		return
	}
	defer r.MultipartForm.RemoveAll()
	f, _, e := r.FormFile("file")
	if e != nil {
		failure(w, 400, "Choose a PNG, JPEG or GIF file.")
		return
	}
	defer f.Close()
	attribution := strings.TrimSpace(r.FormValue("attribution"))
	if len(attribution) > 2000 {
		failure(w, 400, "Attribution is too long.")
		return
	}
	temp, e := os.MkdirTemp("", "gacha-upload-")
	if e != nil {
		internal(w, e)
		return
	}
	defer os.RemoveAll(temp)
	input := filepath.Join(temp, "input")
	out, e := os.Create(input)
	if e != nil {
		internal(w, e)
		return
	}
	hash := sha256.New()
	n, e := io.Copy(io.MultiWriter(out, hash), io.LimitReader(f, (12<<20)+1))
	closeErr := out.Close()
	if e != nil || closeErr != nil || n > 12<<20 {
		failure(w, 400, "The upload is incomplete or exceeds 12 MiB.")
		return
	}
	probe, e := os.Open(input)
	if e != nil {
		internal(w, e)
		return
	}
	_, format, e := image.DecodeConfig(probe)
	probe.Close()
	if e != nil {
		failure(w, 400, "This file is not a supported image. Use PNG, JPEG or GIF.")
		return
	}
	ext, mime := ".png", "image/png"
	if format == "gif" {
		ext, mime = ".gif", "image/gif"
	}
	output := filepath.Join(temp, "render"+ext)
	if e = gacha.Render(r.Context(), input, output, format == "gif"); e != nil {
		logUploadError(w, e)
		return
	}
	token := uuid.NewString()
	rel := fmt.Sprintf("manual/%d/photo-%s/card%s", id, token, ext)
	storage, e := s.Store.MediaStorage()
	if e != nil {
		internal(w, e)
		return
	}
	if e = storage.PutFile(r.Context(), rel, output, mime); e != nil {
		internal(w, e)
		return
	}
	retainObject := false
	defer func() {
		if !retainObject {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := storage.Delete(cleanupCtx, rel); err != nil {
				log.Printf("catalog upload cleanup failed: %v", err)
			}
		}
	}()
	tx, e := s.Store.DB.BeginTx(r.Context(), nil)
	if e != nil {
		internal(w, e)
		return
	}
	defer tx.Rollback()
	var cid int64
	e = tx.QueryRowContext(r.Context(), `SELECT id FROM gacha_characters WHERE id=$1 AND archived_at IS NULL FOR UPDATE`, id).Scan(&cid)
	if e == sql.ErrNoRows {
		failure(w, 409, "Restore the character before adding photos.")
		return
	}
	if e != nil {
		internal(w, e)
		return
	}
	status := "pending"
	if r.FormValue("approve") == "true" {
		status = "approved"
	}
	var aid int64
	e = tx.QueryRowContext(r.Context(), `INSERT INTO gacha_assets(character_id,provider,external_id,source_url,attribution,sha256,path,media_type,is_extra,status,reviewed_by,reviewed_at) VALUES($1,'manual',$2,'',$3,$4,$5,$6,true,$7,'catalog:admin',now()) ON CONFLICT(character_id,sha256) DO NOTHING RETURNING id`, id, token, attribution, hex.EncodeToString(hash.Sum(nil)), rel, mime, status).Scan(&aid)
	if e == sql.ErrNoRows {
		failure(w, 409, "This photo already exists for this character, possibly in the archive.")
		return
	}
	if e != nil {
		internal(w, e)
		return
	}
	if raw := r.FormValue("replace_id"); raw != "" {
		old, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || status != "approved" {
			failure(w, 400, "Replacement photos must be approved and reference an existing photo.")
			return
		}
		var primary bool
		e = tx.QueryRowContext(r.Context(), `SELECT is_primary FROM gacha_assets WHERE id=$1 AND character_id=$2 AND archived_at IS NULL`, old, id).Scan(&primary)
		if e != nil {
			failure(w, 409, "The photo being replaced is no longer available.")
			return
		}
		_, e = tx.ExecContext(r.Context(), `UPDATE gacha_assets SET archived_at=now(),status='rejected',is_primary=false WHERE id=$1`, old)
		if e == nil && primary {
			_, e = tx.ExecContext(r.Context(), `UPDATE gacha_assets SET is_primary=true WHERE id=$1`, aid)
		}
		if e != nil {
			internal(w, e)
			return
		}
	}
	// A failed Commit can have an ambiguous outcome; retain the object for reconciliation.
	retainObject = true
	if e = tx.Commit(); e != nil {
		internal(w, e)
		return
	}
	respond(w, 201, map[string]int64{"id": aid})
}
func logUploadError(w http.ResponseWriter, e error) {
	log.Printf("catalog upload render: %v", e)
	failure(w, 422, "This image could not be processed. Use an image under 24 megapixels and check that FFmpeg is installed.")
}
