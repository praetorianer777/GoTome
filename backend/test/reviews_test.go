//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func (a *app) share(c *http.Client, on bool) {
	a.t.Helper()
	if status, out, _ := a.call(c, http.MethodPut, "/me/sharing", map[string]any{"shareReviews": on}); status != 200 {
		a.t.Fatalf("sharing: %d %v", status, out)
	}
}

func (a *app) review(c *http.Client, book, body string, private bool) {
	a.t.Helper()
	if status, out, _ := a.call(c, http.MethodPut, "/books/"+book+"/review", map[string]any{"body": body, "private": private}); status != 204 {
		a.t.Fatalf("review: %d %v", status, out)
	}
}

func (a *app) reviewsOf(c *http.Client, book string) map[string]any {
	a.t.Helper()
	status, out, _ := a.call(c, http.MethodGet, "/books/"+book+"/reviews", nil)
	if status != 200 {
		a.t.Fatalf("reviews: %d %v", status, out)
	}
	return out
}

func TestReviewsAreSharedOnlyByThoseWhoChoseToAndNotPrivately(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	anna, _ := a.signedIn("anna", "reader")
	bert, _ := a.signedIn("bert", "reader")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	_, book, _ := a.historyBooks(t, lib)
	b := book.String()

	a.call(anna, http.MethodPut, "/books/"+b+"/reading", map[string]any{"rating": 4})
	a.review(anna, b, "A fine old tale.", false)

	if status, out, _ := a.call(anna, http.MethodGet, "/me/sharing", nil); status != 200 || out["shareReviews"] != false {
		t.Fatalf("sharing is on before anyone chose: %d %v", status, out)
	}
	seen := a.reviewsOf(bert, b)
	if seen["stars"] != 0.0 || len(seen["reviews"].([]any)) != 0 {
		t.Errorf("bert is shown what anna keeps to herself: %v", seen)
	}
	if status, _, _ := a.call(bert, http.MethodGet, "/people/anna/reviews", nil); status != 404 {
		t.Errorf("anna's page while she shares nothing: %d", status)
	}

	a.share(anna, true)
	seen = a.reviewsOf(bert, b)
	reviews := seen["reviews"].([]any)
	if seen["stars"] != 1.0 || seen["average"] != 4.0 || len(reviews) != 1 {
		t.Fatalf("after anna shares: %v", seen)
	}
	if r := reviews[0].(map[string]any); r["author"] != "anna" || r["body"] != "A fine old tale." || r["stars"] != 4.0 {
		t.Errorf("the review: %v", r)
	}
	a.call(bert, http.MethodPut, "/books/"+b+"/reading", map[string]any{"rating": 2})
	a.share(bert, true)
	if seen = a.reviewsOf(bert, b); seen["stars"] != 2.0 || seen["average"] != 3.0 {
		t.Errorf("the mean of anna's and bert's stars: %v", seen)
	}
	if mine := a.reviewsOf(anna, b)["mine"].(map[string]any); mine["body"] != "A fine old tale." || mine["private"] != false {
		t.Errorf("anna's own: %v", mine)
	}
	if status, page, _ := a.call(bert, http.MethodGet, "/people/anna/reviews", nil); status != 200 || len(page["reviews"].([]any)) != 1 {
		t.Errorf("anna's page: %d %v", status, page)
	}

	a.review(anna, b, "A fine old tale.", true)
	seen = a.reviewsOf(bert, b)
	if len(seen["reviews"].([]any)) != 0 || seen["stars"] != 1.0 || seen["average"] != 2.0 {
		t.Errorf("a private review, and its stars, are still shared: %v", seen)
	}
	if status, _, _ := a.call(bert, http.MethodGet, "/people/anna/reviews", nil); status != 404 {
		t.Errorf("anna's page with only a private review: %d", status)
	}
	if mine := a.reviewsOf(anna, b)["mine"].(map[string]any); mine["private"] != true {
		t.Errorf("anna's own: %v", mine)
	}

	a.review(anna, b, "A fine old tale.", false)
	a.share(anna, false)
	seen = a.reviewsOf(bert, b)
	if len(seen["reviews"].([]any)) != 0 || seen["stars"] != 1.0 || seen["average"] != 2.0 {
		t.Errorf("turning sharing off does not hide at once: %v", seen)
	}
	status, off, _ := a.call(bert, http.MethodGet, "/people/anna/reviews", nil)
	_, nobody, _ := a.call(bert, http.MethodGet, "/people/nobody/reviews", nil)
	if status != 404 || errorCode(off) != errorCode(nobody) || off["error"].(map[string]any)["message"] != nobody["error"].(map[string]any)["message"] {
		t.Errorf("a person who shares nothing is told apart from nobody: %d %v / %v", status, off, nobody)
	}
	a.review(anna, b, "", false)
	if got := a.reviewsOf(anna, b); got["mine"] != nil {
		t.Errorf("an empty review stays: %v", got["mine"])
	}
}

func TestReviewsOfAPrivateLibraryAreNeverShownCountedOrListedToThoseWhoDoNotSeeIt(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	member, memberID := a.signedIn("member", "reader")
	stranger, _ := a.signedIn("stranger", "reader")
	other, otherID := a.signedIn("other", "reader")
	vault := a.library(admin, "Vault", "private")
	for _, id := range []string{memberID, otherID} {
		if status, out, _ := a.call(admin, http.MethodPut, "/libraries/"+vault+"/members/"+id, nil); status != 204 {
			t.Fatalf("membership: %d %v", status, out)
		}
	}
	status, up := a.upload(admin, vault, "hidden.pdf", []byte("%PDF-1.4 hidden"))
	if status != 200 {
		t.Fatalf("upload: %d %v", status, up)
	}
	book := up["bookId"].(string)
	a.call(member, http.MethodPut, "/books/"+book+"/reading", map[string]any{"rating": 5})
	a.share(member, true)
	a.review(member, book, "Kept in the vault.", false)

	if got := a.reviewsOf(other, book); got["stars"] != 1.0 || len(got["reviews"].([]any)) != 1 {
		t.Fatalf("a member does not see it: %v", got)
	}
	status, seen, _ := a.call(stranger, http.MethodGet, "/books/"+book+"/reviews", nil)
	twin, nothing, _ := a.call(stranger, http.MethodGet, "/books/"+uuid.NewString()+"/reviews", nil)
	if status != 404 || twin != 404 || seen["error"].(map[string]any)["message"] != nothing["error"].(map[string]any)["message"] {
		t.Errorf("the stranger is answered otherwise than about nothing: %d %v / %d %v", status, seen, twin, nothing)
	}
	if status, _, _ := a.call(stranger, http.MethodGet, "/people/member/reviews", nil); status != 404 {
		t.Errorf("the stranger sees the member's page: %d", status)
	}
	if status, _, _ := a.call(stranger, http.MethodPut, "/books/"+book+"/review", map[string]any{"body": "Guess."}); status != 404 {
		t.Errorf("the stranger writes a review: %d", status)
	}

	if status, out, _ := a.call(admin, http.MethodDelete, "/libraries/"+vault+"/members/"+memberID, nil); status != 204 {
		t.Fatalf("removing: %d %v", status, out)
	}
	if status, _, _ := a.call(member, http.MethodGet, "/books/"+book+"/reviews", nil); status != 404 {
		t.Errorf("the former member still reads the reviews: %d", status)
	}
	if status, _, _ := a.call(member, http.MethodGet, "/people/member/reviews", nil); status != 404 {
		t.Errorf("the former member still lists their review: %d", status)
	}
	if got := a.reviewsOf(other, book); got["stars"] != 1.0 || len(got["reviews"].([]any)) != 1 {
		t.Errorf("a member's review of the book is gone for the others: %v", got)
	}
}

func TestReviewsGoWithTheAccountAndFollowAMergedBook(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	anna, annaID := a.signedIn("anna", "editor")
	bert, _ := a.signedIn("bert", "reader")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	_, one, two := a.historyBooks(t, lib)
	a.share(anna, true)
	a.review(anna, one.String(), "First.", false)
	a.review(anna, two.String(), "Second.", true)

	if status, body, _ := a.call(anna, http.MethodPost, "/books/"+one.String()+"/merge", map[string]any{"from": two.String()}); status != 200 {
		t.Fatalf("merge: %d %v", status, body)
	}
	mine := a.reviewsOf(anna, one.String())["mine"].(map[string]any)
	if mine["body"] != "First.\n\nSecond." || mine["private"] != true {
		t.Errorf("after the merge: %v", mine)
	}
	if got := a.reviewsOf(bert, one.String()); len(got["reviews"].([]any)) != 0 {
		t.Errorf("a merge made a private review public: %v", got)
	}

	if _, err := a.pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", annaID); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := a.pool.QueryRow(context.Background(), "SELECT count(*) FROM book_reviews").Scan(&left); err != nil || left != 0 {
		t.Errorf("reviews left after the account: %d %v", left, err)
	}
}

func TestADisabledAccountSharesNothing(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	anna, annaID := a.signedIn("anna", "reader")
	bert, _ := a.signedIn("bert", "reader")
	lib := uuid.MustParse(a.library(admin, "Shelf", "shared"))
	_, book, _ := a.historyBooks(t, lib)
	a.call(anna, http.MethodPut, "/books/"+book.String()+"/reading", map[string]any{"rating": 3})
	a.share(anna, true)
	a.review(anna, book.String(), "Fine.", false)
	if status, out, _ := a.call(admin, http.MethodPatch, "/users/"+annaID, map[string]any{"disabled": true}); status != 200 {
		t.Fatalf("disable: %d %v", status, out)
	}
	if got := a.reviewsOf(bert, book.String()); got["stars"] != 0.0 || len(got["reviews"].([]any)) != 0 {
		t.Errorf("a disabled account still shares: %v", got)
	}
}
