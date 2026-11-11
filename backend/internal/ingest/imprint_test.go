package ingest

import "testing"

func sec(texts ...string) []Section {
	out := make([]Section, len(texts))
	for i, t := range texts {
		out[i] = Section{Text: t}
	}
	return out
}

func TestImprintOf(t *testing.T) {
	// 9780306406157 and 9781861972712 are valid ISBN-13s; 0306406152 is the
	// ISBN-10 of the first.
	cases := []struct {
		name     string
		sections []Section
		want     string
	}{
		{"labelled once", sec("Title page", "Copyright\n\nISBN 978-0-306-40615-7\n\nAll rights reserved"), "9780306406157"},
		{"isbn-10 is made 13", sec("x", "ISBN: 0-306-40615-2"), "9780306406157"},
		{"no word isbn", sec("x", "Order number 978-0-306-40615-7"), ""},
		{"bad checksum", sec("x", "ISBN 978-0-306-40615-8"), ""},
		{"e-book beats print", sec("x", "ISBN 978-1-86197-271-2 (Print)\nISBN 978-0-306-40615-7 (E-Book)"), "9780306406157"},
		{"e-book label before", sec("x", "Print ISBN 978-1-86197-271-2 ISBN E-Book: 978-0-306-40615-7"), "9780306406157"},
		{"print used when alone", sec("x", "Taschenbuch ISBN 978-0-306-40615-7"), "9780306406157"},
		{"print after unlabelled", sec("x", "ISBN 978-1-86197-271-2 Taschenbuch\nISBN 978-0-306-40615-7"), "9780306406157"},
		{"list of other titles", sec("x", "ISBN 978-0-306-40615-7 ISBN 978-1-86197-271-2 ISBN 978-0-14-044913-6 ISBN 978-0-19-953556-9 ISBN 978-0-7432-7356-5"), ""},
		{"head wins over tail", append(sec("x", "ISBN 978-0-306-40615-7"), append(make([]Section, 20), sec("ISBN 978-1-86197-271-2")...)...), "9780306406157"},
		{"two at the end are not trusted", append(append(sec("x"), make([]Section, 20)...), sec("ISBN 978-0-306-40615-7", "ISBN 978-1-86197-271-2")...), ""},
		{"one at the end", append(append(sec("x"), make([]Section, 20)...), sec("ISBN 978-0-306-40615-7")...), "9780306406157"},
	}
	for _, c := range cases {
		if got := ImprintOf(c.sections).ISBN; got != c.want {
			t.Errorf("%s: ISBN %q, want %q", c.name, got, c.want)
		}
	}
}

func TestImprintPublisherAndYear(t *testing.T) {
	cases := []struct {
		text, publisher, year string
	}{
		{"© 2019 Beispielverlag GmbH, Musterstadt\nAlle Rechte vorbehalten", "Beispielverlag GmbH", "2019"},
		{"Copyright © 1999 Example House Press. All rights reserved.", "Example House Press", "1999"},
		{"© 2019 Erika Mustermann", "", "2019"},
		{"1. Auflage 2021\nVerlag: Beispielverlag Musterstadt", "Beispielverlag Musterstadt", "2021"},
		{"First published in 1911 by Example Books Ltd", "", "1911"},
		{"No imprint here", "", ""},
	}
	for _, c := range cases {
		got := ImprintOf(sec(c.text))
		if got.Publisher != c.publisher || got.Year != c.year {
			t.Errorf("%q: got publisher %q year %q, want %q %q", c.text, got.Publisher, got.Year, c.publisher, c.year)
		}
	}
}
