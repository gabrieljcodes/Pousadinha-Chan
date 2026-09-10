// Administrative catalog tool. Run only by an operator with database access.
package main

import (
	"bot/internal/database"
	"bot/internal/gacha"
	"bot/pkg/config"
	"context"
	"flag"
	"fmt"
	"github.com/joho/godotenv"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
)

func main() {
	if e := run(); e != nil {
		log.Fatal(e)
	}
}
func run() error {
	_ = godotenv.Load()
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: gacha import-batch [--force] --job initial-10k --limit 10000 --auto-approve | clear-lease | batch-status <job> | migrate | import <AniList ID> | tag <character ID> <tag> | image <character ID> <Gelbooru post ID> | pending | approve/reject <asset ID> <reviewer> | enable/disable <character ID>")
	}
	config.Load()
	cfg, e := gacha.LoadConfig()
	if e != nil {
		return e
	}
	db := database.NewPostgresDatabase(config.ConnString)
	if e = db.Open(); e != nil {
		return e
	}
	defer db.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	s := &gacha.Store{DB: db.GetDB(), Config: cfg}
	if e = s.Migrate(ctx); e != nil {
		return e
	}
	args := os.Args[2:]
	id := int64(0)
	if len(args) > 0 && os.Args[1] != "import-batch" && os.Args[1] != "batch-status" && os.Args[1] != "clear-lease" {
		id, e = strconv.ParseInt(args[0], 10, 64)
		if e != nil || id <= 0 {
			return fmt.Errorf("positive numeric ID required")
		}
	}
	switch os.Args[1] {
	case "clear-lease":
		_, e = s.DB.ExecContext(ctx, `DELETE FROM gacha_import_leases WHERE provider='anilist'`)
		if e == nil {
			fmt.Println("AniList import lease cleared.")
		}
		return e
	case "import-batch":
		flags := flag.NewFlagSet("import-batch", flag.ContinueOnError)
		job := flags.String("job", "initial-10k", "Persistent job name")
		limit := flags.Int("limit", 10000, "Number of successfully imported characters")
		auto := flags.Bool("auto-approve", false, "Approve AniList portraits and enable eligible characters")
		retry := flags.Bool("retry-failed", false, "Retry failed items when resuming")
		force := flags.Bool("force", false, "Force release any existing stale batch importer lease")
		if e = flags.Parse(args); e != nil {
			return e
		}
		if flags.NArg() != 0 {
			return fmt.Errorf("unexpected batch arguments")
		}
		if *force {
			if _, e = s.DB.ExecContext(ctx, `DELETE FROM gacha_import_leases WHERE provider='anilist'`); e != nil {
				return e
			}
		}
		return s.RunBatch(ctx, gacha.NewAniList(), gacha.BatchOptions{Name: *job, Target: *limit, AutoApprove: *auto, RetryFailed: *retry}, os.Stdout)
	case "batch-status":
		if len(args) != 1 {
			return fmt.Errorf("usage: batch-status <job>")
		}
		stats, e := s.BatchStats(ctx, args[0])
		if e != nil {
			return e
		}
		fmt.Printf("job=%s imported=%d pending=%d skipped=%d failed=%d\n", args[0], stats.Imported, stats.Pending, stats.Skipped, stats.Failed)
	case "migrate":
		fmt.Println("Gacha schema ready (base bot schema must already exist).")
	case "import":
		c, e := gacha.NewAniList().Character(ctx, id)
		if e != nil {
			return e
		}
		cid, e := s.Import(ctx, c)
		if e != nil {
			return e
		}
		fmt.Printf("Character %d: %s (%d works).\n", cid, c.Name, len(c.Works))
		if c.Portrait != "" {
			aid, e := s.AddPortrait(ctx, cid, c)
			if e != nil {
				return fmt.Errorf("metadata imported; portrait failed: %w", e)
			}
			fmt.Printf("Portrait asset %d pending review.\n", aid)
		}
	case "tag":
		if len(args) != 2 {
			return fmt.Errorf("usage: tag <character ID> <exact Gelbooru character tag>")
		}
		_, e = s.DB.ExecContext(ctx, `INSERT INTO gacha_booru_tags(character_id,provider,tag) VALUES($1,'gelbooru',$2) ON CONFLICT(character_id,provider) DO UPDATE SET tag=excluded.tag`, id, args[1])
		return e
	case "image":
		if len(args) != 2 {
			return fmt.Errorf("usage: image <character ID> <Gelbooru post ID>")
		}
		post, e := strconv.ParseInt(args[1], 10, 64)
		if e != nil {
			return e
		}
		aid, e := s.AddBooruAsset(ctx, id, post)
		if e != nil {
			return e
		}
		fmt.Printf("Asset %d downloaded and rendered; pending review.\n", aid)
	case "pending":
		rows, e := s.DB.QueryContext(ctx, `SELECT id,character_id,path,source_url FROM gacha_assets WHERE status='pending' ORDER BY id LIMIT 100`)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var aid, cid int64
			var path, source string
			if e = rows.Scan(&aid, &cid, &path, &source); e != nil {
				return e
			}
			fmt.Printf("%d character=%d local=%s/%s source=%s\n", aid, cid, cfg.MediaDir, path, source)
		}
		return rows.Err()
	case "approve", "reject":
		if len(args) != 2 || args[1] == "" {
			return fmt.Errorf("usage: approve/reject <asset ID> <reviewer identity>")
		}
		status := "approved"
		if os.Args[1] == "reject" {
			status = "rejected"
		}
		res, e := s.DB.ExecContext(ctx, `UPDATE gacha_assets SET status=$2,reviewed_by=$3,reviewed_at=now() WHERE id=$1`, id, status, args[1])
		if e != nil {
			return e
		}
		n, e := res.RowsAffected()
		if e != nil {
			return e
		}
		if n == 0 {
			return fmt.Errorf("asset not found")
		}
		fmt.Println("Review saved. Use enable <character ID> when ready.")
	case "enable", "disable":
		if id <= 0 {
			return fmt.Errorf("character ID required")
		}
		res, e := s.DB.ExecContext(ctx, `UPDATE gacha_characters SET enabled=$2,auto_publish_blocked=NOT $2 WHERE id=$1`, id, os.Args[1] == "enable")
		if e != nil {
			return e
		}
		n, e := res.RowsAffected()
		if e != nil {
			return e
		}
		if n == 0 {
			return fmt.Errorf("character not found")
		}
	default:
		return fmt.Errorf("unknown operation")
	}
	return nil
}
