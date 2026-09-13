package gacha

import (
	"bot/internal/mediastore"
	"context"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"sync"
	"time"
)

type mediaRuntime struct {
	mu     sync.Mutex
	closed bool
	store  *mediastore.Store
	err    error
}

// MediaStorage shares OpenDAL operators across importers and HTTP handlers.
func (s *Store) MediaStorage() (*mediastore.Store, error) {
	s.media.mu.Lock()
	defer s.media.mu.Unlock()
	if s.media.closed {
		return nil, fs.ErrClosed
	}
	if s.media.store == nil && s.media.err == nil {
		cfg := s.Config.Storage
		cfg.Directory = s.Config.MediaDir
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s.media.store, s.media.err = mediastore.New(ctx, cfg)
	}
	return s.media.store, s.media.err
}

// CloseMedia stops new use and waits for active readers before releasing native resources.
func (s *Store) CloseMedia() error {
	s.media.mu.Lock()
	s.media.closed = true
	store := s.media.store
	s.media.mu.Unlock()
	if store != nil {
		return store.Close()
	}
	return nil
}

// ServeMedia serves a key after the caller has checked catalog access. Public
// callers must check approval; admin previews require authentication.
func (s *Store) ServeMedia(w http.ResponseWriter, r *http.Request, key, mime string, private bool) {
	storage, err := s.MediaStorage()
	if err != nil {
		http.Error(w, "Media storage unavailable", http.StatusServiceUnavailable)
		return
	}
	object, err := storage.Open(r.Context(), key)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrInvalid) {
			http.NotFound(w, r)
		} else {
			http.Error(w, "Media storage unavailable", http.StatusServiceUnavailable)
		}
		return
	}
	defer object.Close()
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if private {
		w.Header().Set("Cache-Control", "private, no-store")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=300")
	}
	if object.ETag != "" {
		w.Header().Set("ETag", object.ETag)
	}
	http.ServeContent(w, r, path.Base(key), object.Modified, object)
}
