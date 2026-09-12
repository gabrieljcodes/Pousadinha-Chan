package catalogweb

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

const characterFields = `c.id,c.name,c.native_name,c.aliases,c.gender,c.description,c.favourites,c.enabled,c.editorial_favorite,c.archived_at,c.updated_at,
 greatest(10,floor(c.favourites::numeric*0.015)) AS base_value,
 (SELECT count(*) FROM gacha_assets x WHERE x.character_id=c.id AND x.archived_at IS NULL) AS asset_count,
 (SELECT id FROM gacha_assets x WHERE x.character_id=c.id AND x.archived_at IS NULL ORDER BY x.is_primary DESC,(x.status='approved') DESC,x.id LIMIT 1) AS cover_id,
 COALESCE((SELECT w.title FROM gacha_character_works cw JOIN gacha_works w ON w.id=cw.work_id WHERE cw.character_id=c.id ORDER BY CASE cw.role WHEN 'MAIN' THEN 0 ELSE 1 END,w.id LIMIT 1),'Original') AS work`

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	if page > 100000 {
		page = 100000
	}
	search := strings.TrimSpace(q.Get("q"))
	if len(search) > 200 {
		failure(w, 400, "Search is too long.")
		return
	}
	filter := ` WHERE ($1='' OR c.name ILIKE '%'||$1||'%' OR c.native_name ILIKE '%'||$1||'%' OR c.aliases::text ILIKE '%'||$1||'%' OR c.id::text=$1 OR EXISTS(SELECT 1 FROM gacha_character_works cw JOIN gacha_works w ON w.id=cw.work_id WHERE cw.character_id=c.id AND w.title ILIKE '%'||$1||'%'))
 AND ($2='' OR lower(c.gender)=$2)
 AND ($3='' OR EXISTS(SELECT 1 FROM gacha_character_works cw JOIN gacha_works w ON w.id=cw.work_id WHERE cw.character_id=c.id AND w.kind=$3))
 AND ($4<>'favorites' OR c.editorial_favorite)
 AND (($4='archived' AND c.archived_at IS NOT NULL) OR ($4<>'archived' AND c.archived_at IS NULL))
 AND ($5='' OR ($5='enabled' AND c.enabled) OR ($5='disabled' AND NOT c.enabled) OR ($5='missing' AND NOT EXISTS(SELECT 1 FROM gacha_assets x WHERE x.character_id=c.id AND x.status='approved')) OR ($5='pending' AND EXISTS(SELECT 1 FROM gacha_assets x WHERE x.character_id=c.id AND x.status='pending' AND x.archived_at IS NULL)))`
	args := []any{search, q.Get("gender"), q.Get("kind"), q.Get("view"), q.Get("status")}
	// One statement keeps the count and page consistent even during imports.
	order := map[string]string{"name": "c.name,c.id", "newest": "c.created_at DESC,c.id DESC", "likes": "c.favourites DESC,c.id", "photos": "asset_count DESC,c.id"}[q.Get("sort")]
	if order == "" {
		order = "c.favourites DESC,c.id"
	}
	query := `WITH filtered AS (SELECT c.* FROM gacha_characters c` + filter + `) SELECT jsonb_build_object('total',(SELECT count(*) FROM filtered),'page',$6::int,'page_size',36,'items',COALESCE((SELECT jsonb_agg(to_jsonb(result)) FROM (SELECT ` + characterFields + ` FROM filtered c ORDER BY ` + order + ` LIMIT 36 OFFSET ($6::int-1)*36) result),'[]'::jsonb))`
	args = append(args, page)
	var raw json.RawMessage
	if e := s.Store.DB.QueryRowContext(r.Context(), query, args...).Scan(&raw); e != nil {
		internal(w, e)
		return
	}
	respond(w, 200, raw)
}
func (s *Server) detail(w http.ResponseWriter, r *http.Request) {
	id := pathID(w, r)
	if id == 0 {
		return
	}
	var raw json.RawMessage
	e := s.Store.DB.QueryRowContext(r.Context(), `SELECT to_jsonb(detail) FROM (SELECT `+characterFields+`,
 COALESCE((SELECT jsonb_agg(to_jsonb(w) ORDER BY w.id) FROM gacha_character_works cw JOIN gacha_works w ON w.id=cw.work_id WHERE cw.character_id=c.id),'[]'::jsonb) AS works,
 COALESCE((SELECT jsonb_agg(to_jsonb(src)) FROM gacha_character_sources src WHERE src.character_id=c.id),'[]'::jsonb) AS sources,
 COALESCE((SELECT jsonb_agg(jsonb_build_object('id',a.id,'provider',a.provider,'source_url',a.source_url,'attribution',a.attribution,'status',a.status,'is_primary',a.is_primary,'is_extra',a.is_extra,'editorial_favorite',a.editorial_favorite,'archived_at',a.archived_at,'media_type',a.media_type) ORDER BY a.is_primary DESC,a.id) FROM gacha_assets a WHERE a.character_id=c.id),'[]'::jsonb) AS assets,
 (SELECT count(*) FROM gacha_collection col WHERE col.character_id=c.id) AS owners
 FROM gacha_characters c WHERE c.id=$1) detail`, id).Scan(&raw)
	if e == sql.ErrNoRows {
		failure(w, 404, "Character not found.")
		return
	}
	if e != nil {
		internal(w, e)
		return
	}
	respond(w, 200, raw)
}

type editCharacter struct {
	Name        string   `json:"name"`
	NativeName  string   `json:"native_name"`
	Aliases     []string `json:"aliases"`
	Gender      string   `json:"gender"`
	Description string   `json:"description"`
	Favourites  int      `json:"favourites"`
	UpdatedAt   string   `json:"updated_at"`
	Work        *struct {
		Title   string   `json:"title"`
		Kind    string   `json:"kind"`
		Genres  []string `json:"genres"`
		Studios []string `json:"studios"`
	} `json:"new_work"`
}

func (s *Server) save(w http.ResponseWriter, r *http.Request) {
	var in editCharacter
	if !decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if len(in.Name) < 1 || len(in.Name) > 250 || len(in.NativeName) > 250 || len(in.Description) > 20000 || in.Favourites < 0 || in.Favourites > 2147483647 || len(in.Aliases) > 40 {
		failure(w, 400, "Check the name, description, aliases and non-negative likes.")
		return
	}
	switch in.Gender {
	case "", "Female", "Male", "Non-binary", "Other":
	default:
		failure(w, 400, "Choose a valid gender.")
		return
	}
	for _, a := range in.Aliases {
		if len(a) > 250 {
			failure(w, 400, "An alias is too long.")
			return
		}
	}
	if in.Work != nil {
		in.Work.Title = strings.TrimSpace(in.Work.Title)
		if len(in.Work.Title) < 1 || len(in.Work.Title) > 250 || len(in.Work.Genres) > 30 || len(in.Work.Studios) > 30 {
			failure(w, 400, "Check the new work title and metadata.")
			return
		}
		switch in.Work.Kind {
		case "anime", "manga", "game", "other":
		default:
			failure(w, 400, "Choose a valid work type.")
			return
		}
	}
	tx, e := s.Store.DB.BeginTx(r.Context(), nil)
	if e != nil {
		internal(w, e)
		return
	}
	defer tx.Rollback()
	aliases, _ := json.Marshal(in.Aliases)
	if in.Aliases == nil {
		aliases = []byte("[]")
	}
	var id int64
	if r.Method == "POST" {
		e = tx.QueryRowContext(r.Context(), `INSERT INTO gacha_characters(name,native_name,aliases,gender,description,favourites) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, in.Name, in.NativeName, string(aliases), in.Gender, in.Description, in.Favourites).Scan(&id)
	} else {
		id = pathID(w, r)
		if id == 0 {
			return
		}
		if in.UpdatedAt == "" {
			failure(w, 400, "Reload this character before saving.")
			return
		}
		e = tx.QueryRowContext(r.Context(), `UPDATE gacha_characters SET name=$2,native_name=$3,aliases=$4,gender=$5,description=$6,favourites=$7,updated_at=clock_timestamp() WHERE id=$1 AND updated_at=$8::timestamptz RETURNING id`, id, in.Name, in.NativeName, string(aliases), in.Gender, in.Description, in.Favourites, in.UpdatedAt).Scan(&id)
	}
	if e == sql.ErrNoRows {
		failure(w, 409, "This character changed in another session. Reload before saving.")
		return
	}
	if e != nil {
		internal(w, e)
		return
	}
	if in.Work != nil {
		genres, _ := json.Marshal(in.Work.Genres)
		studios, _ := json.Marshal(in.Work.Studios)
		var wid int64
		e = tx.QueryRowContext(r.Context(), `INSERT INTO gacha_works(provider,external_id,kind,title,genres,studios,source_url) VALUES('manual',$1,$2,$3,COALESCE(NULLIF($4::jsonb,'null'::jsonb),'[]'::jsonb),COALESCE(NULLIF($5::jsonb,'null'::jsonb),'[]'::jsonb),'') RETURNING id`, uuid.NewString(), in.Work.Kind, in.Work.Title, string(genres), string(studios)).Scan(&wid)
		if e == nil {
			_, e = tx.ExecContext(r.Context(), `INSERT INTO gacha_character_works(character_id,work_id,role) VALUES($1,$2,'MAIN')`, id, wid)
		}
		if e != nil {
			internal(w, e)
			return
		}
	}
	if e = tx.Commit(); e != nil {
		internal(w, e)
		return
	}
	respond(w, 200, map[string]int64{"id": id})
}
func (s *Server) characterAction(w http.ResponseWriter, r *http.Request) {
	id := pathID(w, r)
	if id == 0 {
		return
	}
	var in struct {
		Action string `json:"action"`
		Value  bool   `json:"value"`
	}
	if !decode(w, r, &in) {
		return
	}
	var query string
	switch in.Action {
	case "favorite":
		query = `UPDATE gacha_characters SET editorial_favorite=$2,updated_at=clock_timestamp() WHERE id=$1`
	case "archive":
		query = `UPDATE gacha_characters SET archived_at=CASE WHEN $2 THEN now() ELSE NULL END,enabled=false,auto_publish_blocked=true,updated_at=clock_timestamp() WHERE id=$1`
	case "enable":
		query = `UPDATE gacha_characters SET enabled=$2,auto_publish_blocked=NOT $2,updated_at=clock_timestamp() WHERE id=$1 AND (NOT $2 OR (archived_at IS NULL AND EXISTS(SELECT 1 FROM gacha_assets WHERE character_id=$1 AND status='approved')))`
	default:
		failure(w, 400, "Unknown character action.")
		return
	}
	result, e := s.Store.DB.ExecContext(r.Context(), query, id, in.Value)
	if e != nil {
		internal(w, e)
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		failure(w, 409, "Character not found, archived, or missing an approved photo.")
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	e := s.Store.DB.QueryRowContext(r.Context(), `SELECT jsonb_build_object('characters',count(*) FILTER(WHERE archived_at IS NULL),'favorites',count(*) FILTER(WHERE editorial_favorite AND archived_at IS NULL),'archived',count(*) FILTER(WHERE archived_at IS NOT NULL),'enabled',count(*) FILTER(WHERE enabled),'photos',(SELECT count(*) FROM gacha_assets WHERE archived_at IS NULL),'pending',(SELECT count(*) FROM gacha_assets WHERE status='pending' AND archived_at IS NULL)) FROM gacha_characters`).Scan(&raw)
	if e != nil {
		internal(w, e)
		return
	}
	respond(w, 200, raw)
}
