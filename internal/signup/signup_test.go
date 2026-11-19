package signup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestEveryProviderHasFixtures is the guard that makes "add a provider" mean
// "add a provider and its golden deliveries": every registered provider needs
// at least one testdata/<name>/*.json body with its *.want.json.
func TestEveryProviderHasFixtures(t *testing.T) {
	for _, p := range All() {
		if len(fixtures(t, p.Name)) == 0 {
			t.Errorf("provider %q has no fixtures: add testdata/%s/<case>.json (a real delivery body) and <case>.want.json", p.Name, p.Name)
		}
	}
}

// TestProviders parses every fixture with its provider's Parse and compares
// the result with <case>.want.json: a Signup without Source and Raw (the
// caller sets those), or the string "ignored" for ErrIgnored.
func TestProviders(t *testing.T) {
	for _, p := range All() {
		for _, body := range fixtures(t, p.Name) {
			name := strings.TrimSuffix(filepath.Base(body), ".json")
			t.Run(p.Name+"/"+name, func(t *testing.T) {
				raw, err := os.ReadFile(body)
				if err != nil {
					t.Fatal(err)
				}
				wantRaw, err := os.ReadFile(strings.TrimSuffix(body, ".json") + ".want.json")
				if err != nil {
					t.Fatal(err)
				}
				got, err := p.Parse(Delivery{Body: raw})
				if strings.TrimSpace(string(wantRaw)) == `"ignored"` {
					if !errors.Is(err, ErrIgnored) {
						t.Fatalf("got %+v, %v; want ErrIgnored", got, err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				var want Signup
				if err := json.Unmarshal(wantRaw, &want); err != nil {
					t.Fatal(err)
				}
				got.Raw = nil
				got.OccurredAt = got.OccurredAt.UTC()
				if !reflect.DeepEqual(got, want) {
					t.Errorf("got  %+v\nwant %+v", got, want)
				}
			})
		}
	}
}

// fixtures returns the delivery bodies in testdata/<provider>, without the
// .want.json files.
func fixtures(t *testing.T, provider string) []string {
	t.Helper()
	all, err := filepath.Glob(filepath.Join("testdata", provider, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	var bodies []string
	for _, f := range all {
		if !strings.HasSuffix(f, ".want.json") {
			bodies = append(bodies, f)
		}
	}
	return bodies
}
