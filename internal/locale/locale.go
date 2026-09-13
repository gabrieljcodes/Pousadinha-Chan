// Package locale owns the bot's immutable, embedded message catalogs.
// Command identifiers, component IDs and persisted values are not translated.
package locale

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/bwmarrin/discordgo"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed messages/*.json
var catalogs embed.FS

var (
	englishToID      = make(map[string]string)
	bundle           = loadBundle()
	defaultLocalizer = For(os.Getenv("BOT_LANGUAGE"), "en")

	guildLanguageResolver func(guildID string) string
	resolverMu            sync.RWMutex

	activeInteractions int64
	currentLocalizers  sync.Map
)

type contextKey struct{}

var localizerKey = contextKey{}

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
		raw, err := catalogs.ReadFile(path)
		if err != nil {
			panic(fmt.Errorf("read translation %s: %w", path, err))
		}
		if _, err := b.ParseMessageFileBytes(raw, path); err != nil {
			panic(fmt.Errorf("load translation %s: %w", path, err))
		}
		if strings.HasSuffix(path, ".en.json") {
			var entries map[string]map[string]string
			if err := json.Unmarshal(raw, &entries); err == nil {
				for id, val := range entries {
					if other, ok := val["other"]; ok && other != "" {
						englishToID[other] = id
					}
				}
			}
		}
	}
	return b
}

// SetGuildLanguageResolver registers a function to look up a guild's forced language preference.
func SetGuildLanguageResolver(fn func(guildID string) string) {
	resolverMu.Lock()
	guildLanguageResolver = fn
	resolverMu.Unlock()
}

func getGuildLanguage(guildID string) string {
	resolverMu.RLock()
	fn := guildLanguageResolver
	resolverMu.RUnlock()
	if fn != nil && guildID != "" {
		return fn(guildID)
	}
	return "auto"
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

func getGID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	s := buf[:n]
	prefix := []byte("goroutine ")
	if !bytes.HasPrefix(s, prefix) {
		return 0
	}
	s = s[len(prefix):]
	idx := bytes.IndexByte(s, ' ')
	if idx < 0 {
		return 0
	}
	id, _ := strconv.ParseUint(string(s[:idx]), 10, 64)
	return id
}

func currentLocalizer() (Localizer, bool) {
	if atomic.LoadInt64(&activeInteractions) <= 0 {
		return defaultLocalizer, false
	}
	gid := getGID()
	if val, ok := currentLocalizers.Load(gid); ok {
		return val.(Localizer), true
	}
	return defaultLocalizer, false
}

// EnterInteraction binds an interaction-scoped Localizer to the current goroutine.
// It returns a cleanup function that MUST be deferred:
//
//	defer locale.EnterInteraction(i)()
func EnterInteraction(i *discordgo.InteractionCreate) func() {
	if i == nil {
		return func() {}
	}
	loc := FromInteraction(i)
	gid := getGID()
	currentLocalizers.Store(gid, loc)
	atomic.AddInt64(&activeInteractions, 1)
	return func() {
		currentLocalizers.Delete(gid)
		atomic.AddInt64(&activeInteractions, -1)
	}
}

// Text localizes message id using the active interaction localizer on the current goroutine,
// falling back to defaultLocalizer.
func Text(id string, data ...Data) string {
	if loc, ok := currentLocalizer(); ok {
		return loc.Text(id, data...)
	}
	return defaultLocalizer.Text(id, data...)
}

// Plural localizes a plural message using the active interaction localizer on the current goroutine,
// falling back to defaultLocalizer.
func Plural(id string, count int, data Data) string {
	if loc, ok := currentLocalizer(); ok {
		return loc.Plural(id, count, data)
	}
	return defaultLocalizer.Plural(id, count, data)
}

// ResolveLanguages computes the priority chain of language codes for an interaction.
// Priority:
// 1. Guild language explicitly set by an admin (overrides BOT_LANGUAGE if defined and != "auto").
// 2. BOT_LANGUAGE from environment / config (if defined and != "auto").
// 3. Invoking user's Discord client Locale (i.Locale).
// 4. Guild's Discord server Locale (*i.GuildLocale).
// 5. English ("en").
func ResolveLanguages(i *discordgo.InteractionCreate) []string {
	var langs []string

	// 1. Guild language set in DB (highest priority if defined and not "auto")
	if i != nil && i.GuildID != "" {
		guildLang := getGuildLanguage(i.GuildID)
		if guildLang != "" && guildLang != "auto" {
			langs = append(langs, guildLang)
			langs = append(langs, "en")
			return langs
		}
	}

	// 2. BOT_LANGUAGE from env/config (if forced)
	if envLang := os.Getenv("BOT_LANGUAGE"); envLang != "" && envLang != "auto" {
		langs = append(langs, envLang)
		langs = append(langs, "en")
		return langs
	}

	// 3. User's client language
	if i != nil && i.Locale != "" {
		langs = append(langs, string(i.Locale))
	}

	// 4. Guild's server locale
	if i != nil && i.GuildLocale != nil && *i.GuildLocale != "" {
		langs = append(langs, string(*i.GuildLocale))
	}

	// 5. Fallback
	langs = append(langs, "en")
	return langs
}

// FromInteraction extracts language preferences from a Discord interaction.
func FromInteraction(i *discordgo.InteractionCreate) Localizer {
	if i == nil || i.Interaction == nil {
		return defaultLocalizer
	}
	return For(ResolveLanguages(i)...)
}

// WithLocalizer returns a Context carrying the given Localizer.
func WithLocalizer(ctx context.Context, loc Localizer) context.Context {
	return context.WithValue(ctx, localizerKey, loc)
}

// FromContext extracts the Localizer from ctx, falling back to current goroutine localizer or defaultLocalizer.
func FromContext(ctx context.Context) Localizer {
	if ctx != nil {
		if loc, ok := ctx.Value(localizerKey).(Localizer); ok {
			return loc
		}
	}
	if loc, ok := currentLocalizer(); ok {
		return loc
	}
	return defaultLocalizer
}

// InteractionContext returns a context.Context carrying the interaction's preferred Localizer.
func InteractionContext(ctx context.Context, i *discordgo.InteractionCreate) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return WithLocalizer(ctx, FromInteraction(i))
}

// TextCtx localizes message id using the Localizer in ctx, falling back to defaultLocalizer.
func TextCtx(ctx context.Context, id string, data ...Data) string {
	return FromContext(ctx).Text(id, data...)
}

// PluralCtx localizes a plural message using the Localizer in ctx, falling back to defaultLocalizer.
func PluralCtx(ctx context.Context, id string, count int, data Data) string {
	return FromContext(ctx).Plural(id, count, data)
}

// TextI localizes a message directly for a Discord interaction.
func TextI(i *discordgo.InteractionCreate, id string, data ...Data) string {
	return FromInteraction(i).Text(id, data...)
}

// PluralI localizes a plural message directly for a Discord interaction.
func PluralI(i *discordgo.InteractionCreate, id string, count int, data Data) string {
	return FromInteraction(i).Plural(id, count, data)
}

// DescriptionLocalizations returns all translated descriptions for a given English text.
// It maps the English text to its message ID and queries any loaded non-English catalogs.
func DescriptionLocalizations(englishText string) *map[discordgo.Locale]string {
	id, ok := englishToID[englishText]
	if !ok {
		return nil
	}
	m := make(map[discordgo.Locale]string)
	for _, tag := range bundle.LanguageTags() {
		if tag == language.English || tag == language.AmericanEnglish || tag == language.BritishEnglish {
			continue
		}
		loc := i18n.NewLocalizer(bundle, tag.String())
		if msg, err := loc.Localize(&i18n.LocalizeConfig{MessageID: id}); err == nil && msg != "" && msg != englishText {
			dLoc := toDiscordLocale(tag.String())
			if dLoc != "" {
				m[dLoc] = msg
			}
		}
	}
	if len(m) == 0 {
		return nil
	}
	return &m
}

// CommandNameLocalizations returns translated names for a slash command if defined in the catalog.
// Message key convention: "commands.names.<name>"
func CommandNameLocalizations(commandName string) *map[discordgo.Locale]string {
	id := "commands.names." + strings.ToLower(commandName)
	m := make(map[discordgo.Locale]string)
	for _, tag := range bundle.LanguageTags() {
		if tag == language.English || tag == language.AmericanEnglish || tag == language.BritishEnglish {
			continue
		}
		loc := i18n.NewLocalizer(bundle, tag.String())
		if msg, err := loc.Localize(&i18n.LocalizeConfig{MessageID: id}); err == nil && msg != "" && msg != commandName {
			dLoc := toDiscordLocale(tag.String())
			if dLoc != "" {
				m[dLoc] = msg
			}
		}
	}
	if len(m) == 0 {
		return nil
	}
	return &m
}

// OptionDescriptionLocalizations returns translated descriptions for command options.
func OptionDescriptionLocalizations(englishText string) map[discordgo.Locale]string {
	res := DescriptionLocalizations(englishText)
	if res == nil {
		return nil
	}
	return *res
}

// ChoiceNameLocalizations returns translated names for command option choices.
func ChoiceNameLocalizations(englishText string) map[discordgo.Locale]string {
	res := DescriptionLocalizations(englishText)
	if res == nil {
		return nil
	}
	return *res
}

func toDiscordLocale(tag string) discordgo.Locale {
	switch strings.ToLower(tag) {
	case "pt", "pt-br":
		return discordgo.PortugueseBR
	case "es", "es-es":
		return discordgo.SpanishES
	case "fr":
		return discordgo.French
	case "de":
		return discordgo.German
	case "it":
		return discordgo.Italian
	case "ja":
		return discordgo.Japanese
	case "ko":
		return discordgo.Korean
	case "ru":
		return discordgo.Russian
	case "zh-cn":
		return discordgo.ChineseCN
	case "zh-tw":
		return discordgo.ChineseTW
	default:
		return discordgo.Locale(tag)
	}
}
