package gacha

import (
	"bot/migrations"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

type Store struct {
	DB     *sql.DB
	Config Config
}

func (s *Store) Migrate(ctx context.Context) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(72410814)`); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, migrations.Gacha); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, migrations.GachaImport); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, migrations.GachaImportPayload); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, migrations.GachaAssetsExtra); e != nil {
		return e
	}
	return tx.Commit()
}

type Work struct {
	ExternalID, Kind, Title, NativeTitle, URL, Role string
	Genres, Studios                                 []string
}
type Character struct {
	ID                                                                         int64
	Provider, ExternalID, Name, NativeName, Gender, Description, URL, Portrait string
	Aliases                                                                    []string
	Favourites                                                                 int
	Works                                                                      []Work
}

// CatalogProvider can also be implemented by a future game metadata adapter.
type CatalogProvider interface {
	Character(context.Context, int64) (Character, error)
}

func jsonArray(v []string) string {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}
func (s *Store) Import(ctx context.Context, c Character) (int64, error) {
	if c.Provider == "" || c.ExternalID == "" || c.Name == "" {
		return 0, fmt.Errorf("incomplete character")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	// Serialize imports for the same source identity without conflating names.
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, c.Provider+":"+c.ExternalID); e != nil {
		return 0, e
	}
	var id int64
	e = tx.QueryRowContext(ctx, `SELECT character_id FROM gacha_character_sources WHERE provider=$1 AND external_id=$2`, c.Provider, c.ExternalID).Scan(&id)
	if e == sql.ErrNoRows {
		e = tx.QueryRowContext(ctx, `INSERT INTO gacha_characters(name) VALUES($1) RETURNING id`, c.Name).Scan(&id)
	}
	if e != nil {
		return 0, e
	}
	_, e = tx.ExecContext(ctx, `UPDATE gacha_characters SET name=$2,native_name=$3,aliases=$4,gender=$5,description=$6,favourites=$7,updated_at=now() WHERE id=$1`, id, c.Name, c.NativeName, jsonArray(c.Aliases), c.Gender, c.Description, c.Favourites)
	if e != nil {
		return 0, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO gacha_character_sources(provider,external_id,character_id,source_url) VALUES($1,$2,$3,$4) ON CONFLICT(provider,external_id) DO UPDATE SET source_url=excluded.source_url,synced_at=now()`, c.Provider, c.ExternalID, id, c.URL)
	if e != nil {
		return 0, e
	}
	// Only replace this provider's relationships; future game associations survive refreshes.
	_, e = tx.ExecContext(ctx, `DELETE FROM gacha_character_works cw USING gacha_works w WHERE cw.work_id=w.id AND cw.character_id=$1 AND w.provider=$2`, id, c.Provider)
	if e != nil {
		return 0, e
	}
	for _, w := range c.Works {
		var wid int64
		e = tx.QueryRowContext(ctx, `INSERT INTO gacha_works(provider,external_id,kind,title,native_title,genres,studios,source_url) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(provider,external_id) DO UPDATE SET title=excluded.title,native_title=excluded.native_title,genres=excluded.genres,studios=excluded.studios RETURNING id`, c.Provider, w.ExternalID, w.Kind, w.Title, w.NativeTitle, jsonArray(w.Genres), jsonArray(w.Studios), w.URL).Scan(&wid)
		if e != nil {
			return 0, e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO gacha_character_works(character_id,work_id,role) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, id, wid, w.Role)
		if e != nil {
			return 0, e
		}
	}
	return id, tx.Commit()
}
