package annotations

import (
	"cmp"
	"encoding/csv"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
)

// InBookOrder sorts annotations by book, then as the book runs, then as
// they were made.
func InBookOrder(list []Annotation) {
	slices.SortStableFunc(list, func(a, b Annotation) int {
		if c := cmp.Compare(strings.ToLower(a.BookTitle), strings.ToLower(b.BookTitle)); c != 0 {
			return c
		}
		if c := strings.Compare(a.BookID.String(), b.BookID.String()); c != 0 {
			return c
		}
		switch {
		case a.Fraction != nil && b.Fraction != nil:
			if c := cmp.Compare(*a.Fraction, *b.Fraction); c != 0 {
				return c
			}
		case a.Fraction != nil:
			return -1
		case b.Fraction != nil:
			return 1
		}
		return a.CreatedAt.Compare(b.CreatedAt)
	})
}

// Markdown writes the annotations, grouped by book, as a Markdown document.
func Markdown(w io.Writer, list []Annotation) error {
	InBookOrder(list)
	var b strings.Builder
	b.WriteString("# Highlights and notes\n")
	for i, a := range list {
		if i == 0 || a.BookID != list[i-1].BookID {
			fmt.Fprintf(&b, "\n## %s\n", oneLine(a.BookTitle))
			if len(a.BookAuthors) > 0 {
				fmt.Fprintf(&b, "\n*%s*\n", oneLine(strings.Join(a.BookAuthors, ", ")))
			}
		}
		b.WriteString("\n")
		for _, line := range strings.Split(strings.ReplaceAll(a.Passage, "\r", ""), "\n") {
			b.WriteString("> " + line + "\n")
		}
		if a.Note != "" {
			b.WriteString("\n**Note:** " + strings.ReplaceAll(strings.TrimSpace(a.Note), "\n", "  \n") + "\n")
		}
		var where []string
		if a.Chapter != "" {
			where = append(where, oneLine(a.Chapter))
		}
		if a.Page != nil {
			where = append(where, "page "+strconv.Itoa(int(*a.Page)))
		}
		where = append(where, a.CreatedAt.UTC().Format(time.DateOnly))
		b.WriteString("\n<sub>" + strings.Join(where, " · ") + "</sub>\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// cell keeps a spreadsheet from running what a book's text starts with.
func cell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// CSV writes the annotations as rows, with a header, in the order Markdown
// has them.
func CSV(w io.Writer, list []Annotation) error {
	InBookOrder(list)
	out := csv.NewWriter(w)
	if err := out.Write([]string{"Title", "Authors", "Chapter", "Page", "Passage", "Note", "Color", "Created"}); err != nil {
		return err
	}
	for _, a := range list {
		page := ""
		if a.Page != nil {
			page = strconv.Itoa(int(*a.Page))
		}
		if err := out.Write([]string{
			cell(a.BookTitle), cell(strings.Join(a.BookAuthors, "; ")), cell(a.Chapter), page,
			cell(a.Passage), cell(a.Note), a.Color, a.CreatedAt.UTC().Format(time.RFC3339),
		}); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}
