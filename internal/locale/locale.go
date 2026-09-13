// Package locale owns the bot's immutable, embedded message catalogs.
// Command identifiers, component IDs and persisted values are not translated.
package locale

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed messages/*.json
var catalogs embed.FS

var bundle = loadBundle()
var defaultLocalizer = For(os.Getenv("BOT_LANGUAGE"), "en")

type Data map[string]any

type Localizer struct{ localizer *i18n.Localizer }

func loadBundle() *i18n.Bundle {
	b := i18n.NewBundle(language.English)
	b.RegisterUnmarshalFunc("json", json.Unmarshal)
	paths, err := fs.Glob(catalogs, "messages/*.json")
	if err != nil {
		panic(err)
	}
	for _, path := range paths {
		if _, err := b.LoadMessageFileFS(catalogs, path); err != nil {
			panic(fmt.Errorf("load translation %s: %w", path, err))
		}
	}
	return b
}

// For uses language preferences in order, with English as the fallback.
// Localizers are request-scoped; never change a process-wide language on behalf
// of a Discord user. Only English is shipped in the initial catalog.
func For(languages ...string) Localizer { return Localizer{i18n.NewLocalizer(bundle, languages...)} }

func (l Localizer) Text(id string, data ...Data) string {
	config := &i18n.LocalizeConfig{MessageID: id}
	if len(data) > 0 {
		config.TemplateData = data[0]
	}
	return l.localizer.MustLocalize(config)
}
func (l Localizer) Plural(id string, count int, data Data) string {
	return l.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: id, PluralCount: count, TemplateData: data})
}

func Text(id string, data ...Data) string { return defaultLocalizer.Text(id, data...) }

func Plural(id string, count int, data Data) string { return defaultLocalizer.Plural(id, count, data) }
