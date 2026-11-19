package crm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// where collects the optional filters of a search query and their args.
type where struct {
	conds []string
	args  []any
}

// arg binds v and returns its placeholder.
func (w *where) arg(v any) string {
	w.args = append(w.args, v)
	return "$" + strconv.Itoa(len(w.args))
}

func (w *where) add(format string, placeholders ...any) {
	w.conds = append(w.conds, fmt.Sprintf(format, placeholders...))
}

func (w *where) String() string {
	if len(w.conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(w.conds, " AND ")
}

// contains is an ILIKE pattern matching s anywhere, with s's own % and _
// matched literally.
func contains(s string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s) + "%"
}

func pageLimit(n int) int {
	switch {
	case n <= 0:
		return 50
	case n > 200:
		return 200
	}
	return n
}

// pageOf trims a query that fetched limit+1 rows to a page, setting
// NextCursor from the last kept item when more rows exist.
func pageOf[T any](items []T, limit int, cursor func(T) string) Page[T] {
	if items == nil {
		items = []T{}
	}
	if len(items) <= limit {
		return Page[T]{Items: items}
	}
	return Page[T]{Items: items[:limit], NextCursor: cursor(items[limit-1])}
}

// A cursor is the keyset position (timestamp, id) of the last item on a
// page, in an opaque encoding.
func encodeCursor(t time.Time, id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(t.UnixMicro(), 10) + "." + strconv.FormatInt(id, 10)))
}

func decodeCursor(s string) (time.Time, int64, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	ts, id, ok := strings.Cut(string(b), ".")
	micros, err1 := strconv.ParseInt(ts, 10, 64)
	n, err2 := strconv.ParseInt(id, 10, 64)
	if err != nil || !ok || err1 != nil || err2 != nil {
		return time.Time{}, 0, fmt.Errorf("%w: bad cursor", ErrInvalid)
	}
	return time.UnixMicro(micros), n, nil
}

// pgCode is the SQLSTATE of a Postgres error, or "".
func pgCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

const (
	foreignKeyViolation = "23503"
	uniqueViolation     = "23505"
	checkViolation      = "23514"
)

// activityRow is one timeline entry to insert. Exactly one of contact and
// company is non-zero.
type activityRow struct {
	contact ContactID
	company CompanyID
	kind    ActivityKind
	actor   Actor
	dedupe  string // natural key such as "signed_up:clerk:user_1"; "" for none
	data    any
	at      time.Time
}

// recordActivity inserts a, or nothing when its dedupe key already exists,
// and reports whether a row was inserted.
func recordActivity(ctx context.Context, tx pgx.Tx, a activityRow) (bool, error) {
	data, err := json.Marshal(a.data)
	if err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO activities (contact_id, company_id, kind, actor_kind, actor_id, actor_label, dedupe_key, data, occurred_at)
		VALUES (NULLIF($1::bigint, 0), NULLIF($2::bigint, 0), $3, $4, $5, $6, NULLIF($7, ''), $8, $9)
		ON CONFLICT (dedupe_key) DO NOTHING`,
		a.contact, a.company, a.kind, a.actor.Kind, a.actor.ID, a.actor.Label, a.dedupe, json.RawMessage(data), a.at)
	return tag.RowsAffected() == 1, err
}

// change is one field's before and after in an *_updated activity.
type change struct {
	From string `json:"from"`
	To   string `json:"to"`
}
