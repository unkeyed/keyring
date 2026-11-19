package signup

import (
	"os"
	"strings"
	"testing"
)

// The SQL is pasted from two places, the Settings page and the docs; both
// must be the SQL this adapter parses.
func TestSupabaseDocsShowSetupSQL(t *testing.T) {
	doc, err := os.ReadFile("../../docs/providers/supabase.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), "```sql\n"+supabaseSQL+"```") {
		t.Error("docs/providers/supabase.md does not contain internal/signup/supabase.sql verbatim; paste it in again")
	}
}
