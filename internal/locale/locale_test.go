package locale

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestCatalogsAndCallSites(t *testing.T) {
	fields := regexp.MustCompile(`{{\s*(?:printf\s+"[^"]+"\s+)?\.([A-Za-z0-9_]+)\s*}}`)
	messages := map[string]map[string]string{}
	paths, err := fs.Glob(catalogs, "messages/*.en.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		raw, err := catalogs.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var chunk map[string]map[string]string
		if err := json.Unmarshal(raw, &chunk); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for id, forms := range chunk {
			if _, exists := messages[id]; exists {
				t.Fatalf("duplicate message %s in %s", id, path)
			}
			if forms["other"] == "" {
				t.Fatalf("message %s in %s missing other form", id, path)
			}
			messages[id] = forms
		}
	}
	used := map[string]bool{}
	for _, root := range []string{"../", "../../pkg"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				fn, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := fn.X.(*ast.Ident)
				if !ok || (pkg.Name != "locale" && pkg.Name != "loc" && pkg.Name != "l") {
					return true
				}
				method := fn.Sel.Name
				if method != "Text" && method != "Format" && method != "Plural" && method != "TextI" && method != "PluralI" && method != "TextCtx" && method != "PluralCtx" {
					return true
				}
				argIndex := 0
				if method == "TextI" || method == "PluralI" || method == "TextCtx" || method == "PluralCtx" {
					argIndex = 1
				}
				if len(call.Args) <= argIndex {
					return true
				}
				literal, ok := call.Args[argIndex].(*ast.BasicLit)
				if !ok {
					t.Errorf("message IDs must be static in %s", path)
					return true
				}
				id, _ := strconv.Unquote(literal.Value)
				message, exists := messages[id]
				if !exists {
					t.Errorf("missing message %s in %s", id, path)
					return true
				}
				used[id] = true
				required := fields.FindAllStringSubmatch(message["other"], -1)
				provided := map[string]bool{}
				dataIndex := argIndex + 1
				if method == "Plural" || method == "PluralI" || method == "PluralCtx" {
					dataIndex = argIndex + 2
				}
				if len(call.Args) > dataIndex {
					if data, ok := call.Args[dataIndex].(*ast.CompositeLit); ok {
						for _, entry := range data.Elts {
							if kv, ok := entry.(*ast.KeyValueExpr); ok {
								if lit, ok := kv.Key.(*ast.BasicLit); ok {
									key, _ := strconv.Unquote(lit.Value)
									provided[key] = true
								}
							}
						}
					}
				}
				for _, field := range required {
					if !provided[field[1]] {
						t.Errorf("%s: missing template data %s for %s", path, field[1], id)
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for id := range messages {
		if !used[id] {
			t.Errorf("unused catalog message %s", id)
		}
	}
}

func TestEnglishFallbackAndConcurrentLocalizers(t *testing.T) {
	expected := Text("common.server_only")
	var wg sync.WaitGroup
	for n := 0; n < 50; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, lang := range []string{"en", "zz-ZZ", "und", "invalid"} {
				if got := For(lang).Text("common.server_only"); got != expected {
					t.Errorf("fallback for %s: %q", lang, got)
				}
			}
		}()
	}
	wg.Wait()
}

func TestFromInteractionAndContext(t *testing.T) {
	guildLocale := discordgo.PortugueseBR
	i := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Locale:      discordgo.PortugueseBR,
			GuildLocale: &guildLocale,
		},
	}
	expected := Text("common.server_only")
	loc := FromInteraction(i)
	if msg := loc.Text("common.server_only"); msg != expected {
		t.Fatalf("unexpected localized message: %q, want %q", msg, expected)
	}

	ctx := InteractionContext(context.Background(), i)
	if msg := TextCtx(ctx, "common.server_only"); msg != expected {
		t.Fatalf("unexpected TextCtx message: %q, want %q", msg, expected)
	}

	if msg := TextI(i, "common.server_only"); msg != expected {
		t.Fatalf("unexpected TextI message: %q, want %q", msg, expected)
	}
}

func TestDescriptionLocalizations(t *testing.T) {
	// For existing English description, since only English is loaded, should return nil
	res := DescriptionLocalizations("Show all commands and features")
	if res != nil {
		t.Fatalf("expected nil for English-only bundle, got %v", res)
	}

	// Reverse lookup works for known English text
	if id, ok := englishToID["Show all commands and features"]; !ok || id != "commands.definitions.show_all_commands_and_features" {
		t.Fatalf("expected ID commands.definitions.show_all_commands_and_features, got %q (found: %v)", id, ok)
	}
}

func TestPluralForms(t *testing.T) {
	for _, tc := range []struct {
		count int
		want  string
	}{{0, "0/20 wished characters"}, {1, "1/20 wished character"}, {2, "2/20 wished characters"}} {
		if got := Plural("gacha.wishlist.count", tc.count, Data{"Count": tc.count}); got != tc.want {
			t.Errorf("plural %d: %q", tc.count, got)
		}
	}
}
