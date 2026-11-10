package curation

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Snapshot is the rows of the tables of Tables that belong to some books,
// as JSON, sorted by their keys, by table name.
type Snapshot map[string]json.RawMessage

// rows reads a table's rows of a snapshot.
func (s Snapshot) rows(table string) ([]map[string]any, error) {
	var out []map[string]any
	if err := json.Unmarshal(s[table], &out); err != nil {
		return nil, fmt.Errorf("snapshot of %s: %w", table, err)
	}
	return out, nil
}

func (t Table) rowExpr() string {
	if len(t.Columns) == 0 {
		return "to_jsonb(t)"
	}
	parts := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		parts[i] = "'" + c + "', t." + c
	}
	return "jsonb_build_object(" + strings.Join(parts, ", ") + ")"
}

func (t Table) orderBy() string {
	cols := make([]string, len(t.PK))
	for i, c := range t.PK {
		cols[i] = "t." + c
	}
	return strings.Join(cols, ", ")
}

// Capture reads the rows of the books in the tables.
func Capture(ctx context.Context, tx pgx.Tx, books []uuid.UUID, tables []Table) (Snapshot, error) {
	out := Snapshot{}
	for _, t := range tables {
		var raw []byte
		query := fmt.Sprintf(`SELECT coalesce(jsonb_agg(%s ORDER BY %s), '[]'::jsonb) FROM %s t WHERE %s`,
			t.rowExpr(), t.orderBy(), t.Name, t.Where)
		if err := tx.QueryRow(ctx, query, books).Scan(&raw); err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", t.Name, err)
		}
		out[t.Name] = raw
	}
	return out, nil
}

func (t Table) restoredColumns(ctx context.Context, tx pgx.Tx) ([]string, error) {
	if len(t.Columns) > 0 {
		return t.Columns, nil
	}
	rows, err := tx.Query(ctx, `SELECT column_name FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = $1 AND is_generated = 'NEVER'
		ORDER BY ordinal_position`, t.Name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

// Restore puts the rows of the books back as the snapshot has them: rows
// that are gone come back, rows that changed are changed back, and rows
// that were not there go, except in tables that Keep them. The tables are
// restored in the order of Tables.
func Restore(ctx context.Context, tx pgx.Tx, books []uuid.UUID, snap Snapshot) error {
	for _, t := range tablesIn(snap) {
		if !t.Keep {
			var same []string
			for _, c := range t.PK {
				same = append(same, "s."+c+" = t."+c)
			}
			del := fmt.Sprintf(`DELETE FROM %s t WHERE (%s) AND NOT EXISTS (
				SELECT 1 FROM jsonb_populate_recordset(NULL::%s, $2) s WHERE %s)`,
				t.Name, t.Where, t.Name, strings.Join(same, " AND "))
			if _, err := tx.Exec(ctx, del, books, []byte(snap[t.Name])); err != nil {
				return fmt.Errorf("restore %s: %w", t.Name, err)
			}
		}
		cols, err := t.restoredColumns(ctx, tx)
		if err != nil {
			return err
		}
		var stmt string
		if len(t.Columns) > 0 {
			var set, same []string
			for _, c := range cols {
				if !contains(t.PK, c) {
					set = append(set, c+" = s."+c)
				}
			}
			for _, c := range t.PK {
				same = append(same, "s."+c+" = t."+c)
			}
			if t.Touch {
				set = append(set, "updated_at = now()")
			}
			stmt = fmt.Sprintf(`UPDATE %s t SET %s FROM jsonb_populate_recordset(NULL::%s, $1) s WHERE %s`,
				t.Name, strings.Join(set, ", "), t.Name, strings.Join(same, " AND "))
		} else {
			var set []string
			for _, c := range cols {
				if !contains(t.PK, c) {
					set = append(set, c+" = EXCLUDED."+c)
				}
			}
			conflict := "DO NOTHING"
			if len(set) > 0 {
				conflict = "DO UPDATE SET " + strings.Join(set, ", ")
			}
			stmt = fmt.Sprintf(`INSERT INTO %s (%s) SELECT %s FROM jsonb_populate_recordset(NULL::%s, $1) ON CONFLICT (%s) %s`,
				t.Name, quoteList(cols), quoteList(cols), t.Name, quoteList(t.PK), conflict)
		}
		if _, err := tx.Exec(ctx, stmt, []byte(snap[t.Name])); err != nil {
			return fmt.Errorf("restore %s: %w", t.Name, err)
		}
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Conflict is a table whose rows of the books are not as an action left
// them.
type Conflict struct {
	Table string `json:"table"`
	// Rows is how many rows differ, are gone or are new.
	Rows int `json:"rows"`
}

func key(t Table, row map[string]any) string {
	parts := make([]string, len(t.PK))
	for i, c := range t.PK {
		parts[i] = fmt.Sprint(row[c])
	}
	return strings.Join(parts, "|")
}

// Conflicts compares the books' rows now with what an action left, in the
// tables the snapshot holds, leaving out the tables that are Quiet.
func Conflicts(ctx context.Context, tx pgx.Tx, books []uuid.UUID, left Snapshot) ([]Conflict, error) {
	tables := tablesIn(left)
	now, err := Capture(ctx, tx, books, tables)
	if err != nil {
		return nil, err
	}
	var out []Conflict
	for _, t := range tables {
		if t.Quiet {
			continue
		}
		want, err := left.rows(t.Name)
		if err != nil {
			return nil, err
		}
		have, err := now.rows(t.Name)
		if err != nil {
			return nil, err
		}
		settled := func(rows []map[string]any) map[string]map[string]any {
			m := map[string]map[string]any{}
			for _, r := range rows {
				if t.Settled == nil || t.Settled(r) {
					m[key(t, r)] = r
				}
			}
			return m
		}
		w, h := settled(want), settled(have)
		n := 0
		for k, r := range w {
			if other, ok := h[k]; !ok || !reflect.DeepEqual(r, other) {
				n++
			}
		}
		for k := range h {
			if _, ok := w[k]; !ok {
				n++
			}
		}
		if n > 0 {
			out = append(out, Conflict{Table: t.Name, Rows: n})
		}
	}
	return out, nil
}
