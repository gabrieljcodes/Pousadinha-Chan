// Catalog runs the same embedded administrative UI independently of Discord.
package main

import (
	"bot/internal/catalogweb"
	"bot/internal/database"
	"bot/internal/gacha"
	"bot/pkg/config"
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
)

func main() {
	if e := run(); e != nil {
		log.Fatal(e)
	}
}
func run() error {
	_ = godotenv.Load()
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
	store := &gacha.Store{DB: db.GetDB(), Config: cfg}
	admin, e := catalogweb.New(store, os.Getenv("CATALOG_ADMIN_PASSWORD"), os.Getenv("CATALOG_SECURE_COOKIES") == "true")
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	e = store.Migrate(ctx)
	cancel()
	if e != nil {
		return e
	}
	addr := os.Getenv("CATALOG_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8081"
	}
	server := &http.Server{Addr: addr, Handler: admin.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 180 * time.Second, IdleTimeout: 60 * time.Second}
	stop, cancelStop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancelStop()
	go func() {
		<-stop.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	log.Printf("Catalog editor: http://%s/catalog/", addr)
	e = server.ListenAndServe()
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}
