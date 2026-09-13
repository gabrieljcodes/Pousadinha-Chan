package locale

import (
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
)

func TestCatalogsAndCallSites(t *testing.T) {
	messages := map[string]map[string]string{}
	files, _ := fs.Glob(catalogs, "messages/*.en.json")
	fields := regexp.MustCompile(`\{\{printf "([^"]+)" \.(\w+)\}\}`)
	legacy := regexp.MustCompile("[!$][a-zA-Z]+")
	for _, path := range files {
		raw, err := catalogs.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var entries map[string]map[string]string
		if err = json.Unmarshal(raw, &entries); err != nil {
			t.Fatal(err)
		}
		for id, message := range entries {
			if _, exists := messages[id]; exists {
				t.Fatalf("duplicate message %s", id)
			}
			messages[id] = message
			data := Data{}
			for _, m := range fields.FindAllStringSubmatch(message["other"], -1) {
				var value any = "example"
				switch m[1][len(m[1])-1] {
				case 'd', 'b', 'o', 'x', 'X', 'U', 'c':
					value = 2
				case 'f', 'F', 'g', 'G', 'e', 'E':
					value = 2.0
				}
				data[m[2]] = value
			}
			rendered := ""
			if message["one"] != "" {
				rendered = For("en").Plural(id, 2, data)
			} else {
				rendered = Text(id, data)
			}
			if strings.Contains(rendered, "%!") || strings.Contains(rendered, "<no value>") {
				t.Errorf("invalid formatting in %s: %s", id, rendered)
			}
			if legacy.MatchString(message["other"]) {
				t.Errorf("legacy command in %s: %s", id, rendered)
			}
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
				if !ok || pkg.Name != "locale" || (fn.Sel.Name != "Text" && fn.Sel.Name != "Format" && fn.Sel.Name != "Plural") {
					return true
				}
				literal, ok := call.Args[0].(*ast.BasicLit)
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
				if len(call.Args) > 1 {
					dataIndex := 1
					if fn.Sel.Name == "Plural" {
						dataIndex = 2
					}
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
					if !provided[field[2]] {
						t.Errorf("%s: missing template data %s for %s", path, field[2], id)
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
