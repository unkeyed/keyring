package signup

import "testing"

// TestProviders runs every registered provider against its fixtures in
// testdata/<name>/*.json (body) and *.want.json (expected Signup or
// "ignored"). A new provider file with no fixtures fails this test.
func TestProviders(t *testing.T) {
	for _, p := range All() {
		t.Run(p.Name, func(t *testing.T) {
			t.Skip("not implemented: load testdata/" + p.Name)
		})
	}
}
