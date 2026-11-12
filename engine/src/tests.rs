use super::*;

const LIB_A: &str = "00000000-0000-4000-a000-000000000001";
const LIB_B: &str = "00000000-0000-4000-a000-000000000002";
const BOOK_1: &str = "00000000-0000-4000-8000-000000000001";
const BOOK_2: &str = "00000000-0000-4000-8000-000000000002";
const BOOK_3: &str = "00000000-0000-4000-8000-000000000003";

struct TempDir(std::path::PathBuf);

impl TempDir {
    fn new(name: &str) -> TempDir {
        let p = std::env::temp_dir().join(format!("gotome-fulltext-{}-{name}", std::process::id()));
        let _ = std::fs::remove_dir_all(&p);
        TempDir(p)
    }
}

impl Drop for TempDir {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}

fn doc(id: u64, book: &str, library: &str, lang: &str, text: &str) -> Doc {
    Doc {
        id,
        book: book.into(),
        library: library.into(),
        lang: lang.into(),
        text: text.into(),
    }
}

fn fill(e: &Engine) {
    e.apply(&Batch {
        add: vec![
            doc(
                1,
                BOOK_1,
                LIB_A,
                "en",
                "The whales were running before the harpooned ship.",
            ),
            doc(
                2,
                BOOK_1,
                LIB_A,
                "en",
                "Call me Ishmael. Some years ago, never mind how long.",
            ),
            doc(
                3,
                BOOK_2,
                LIB_A,
                "de",
                "In der Nacht gingen die Häuser am Fluss in Flammen auf.",
            ),
            doc(
                4,
                BOOK_3,
                LIB_B,
                "en",
                "A whale runs where the old man stands.",
            ),
            doc(
                5,
                BOOK_3,
                LIB_B,
                "fr",
                "La maison au bord du jardin, la nuit.",
            ),
        ],
        ..Default::default()
    })
    .unwrap();
    e.commit().unwrap();
}

fn search(libs: &[&str]) -> Search {
    Search {
        libraries: libs.iter().map(|s| s.to_string()).collect(),
        limit: 100,
        ..Default::default()
    }
}

fn ids(hits: Vec<Hit>) -> Vec<u64> {
    let mut v: Vec<u64> = hits.into_iter().map(|h| h.id).collect();
    v.sort();
    v
}

#[test]
fn words_match_in_any_form_and_all_must_occur() {
    let dir = TempDir::new("words");
    let e = Engine::open(&dir.0, 20_000_000).unwrap();
    fill(&e);
    let mut q = search(&[LIB_A, LIB_B]);
    q.words = "whale run".into();
    assert_eq!(ids(e.search(&q).unwrap()), vec![1, 4]);
    q.words = "whale ishmael".into();
    assert_eq!(ids(e.search(&q).unwrap()), Vec::<u64>::new());
    q.words = "Häuser Flüssen".into();
    assert_eq!(ids(e.search(&q).unwrap()), vec![3]);
    q.words = "maison jardin".into();
    assert_eq!(ids(e.search(&q).unwrap()), vec![5]);
}

#[test]
fn phrases_keep_their_order() {
    let dir = TempDir::new("phrases");
    let e = Engine::open(&dir.0, 20_000_000).unwrap();
    fill(&e);
    let mut q = search(&[LIB_A, LIB_B]);
    q.phrases = vec!["in der nacht".into()];
    assert_eq!(ids(e.search(&q).unwrap()), vec![3]);
    q.phrases = vec!["der in nacht".into()];
    assert_eq!(ids(e.search(&q).unwrap()), Vec::<u64>::new());
    q.phrases = vec!["the old man".into()];
    q.words = "whale".into();
    assert_eq!(ids(e.search(&q).unwrap()), vec![4]);
}

#[test]
fn groups_and_variants_take_one_of_each() {
    let dir = TempDir::new("groups");
    let e = Engine::open(&dir.0, 20_000_000).unwrap();
    fill(&e);
    let mut q = search(&[LIB_A, LIB_B]);
    q.groups = vec![vec!["ishmal".into(), "ishmael".into()]];
    assert_eq!(ids(e.search(&q).unwrap()), vec![2]);
    q.groups = vec![];
    q.variants = vec![vec!["the olt man".into(), "the old man".into()]];
    assert_eq!(ids(e.search(&q).unwrap()), vec![4]);
}

#[test]
fn only_the_libraries_and_books_named_are_searched() {
    let dir = TempDir::new("scope");
    let e = Engine::open(&dir.0, 20_000_000).unwrap();
    fill(&e);
    let mut q = search(&[LIB_A]);
    q.words = "whale".into();
    assert_eq!(ids(e.search(&q).unwrap()), vec![1]);
    q.libraries = vec![];
    assert_eq!(ids(e.search(&q).unwrap()), Vec::<u64>::new());
    q = search(&[LIB_A, LIB_B]);
    q.words = "whale".into();
    q.books = Some(vec![BOOK_3.into()]);
    assert_eq!(ids(e.search(&q).unwrap()), vec![4]);
    q.books = Some(vec![]);
    assert_eq!(ids(e.search(&q).unwrap()), Vec::<u64>::new());
}

#[test]
fn deletes_take_chunks_books_and_libraries_out() {
    let dir = TempDir::new("deletes");
    let e = Engine::open(&dir.0, 20_000_000).unwrap();
    fill(&e);
    let mut q = search(&[LIB_A, LIB_B]);
    q.words = "the".into();
    assert_eq!(ids(e.search(&q).unwrap()), vec![1, 4]);

    e.apply(&Batch {
        delete_chunks: vec![1],
        ..Default::default()
    })
    .unwrap();
    assert_eq!(
        ids(e.search(&q).unwrap()),
        vec![1, 4],
        "not before the commit"
    );
    e.commit().unwrap();
    assert_eq!(ids(e.search(&q).unwrap()), vec![4]);

    // A book written again: its old chunks go, the new ones stay.
    e.apply(&Batch {
        delete_books: vec![BOOK_3.into()],
        add: vec![doc(6, BOOK_3, LIB_B, "en", "the new text")],
        ..Default::default()
    })
    .unwrap();
    e.commit().unwrap();
    assert_eq!(ids(e.search(&q).unwrap()), vec![6]);

    e.apply(&Batch {
        delete_libraries: vec![LIB_B.into()],
        ..Default::default()
    })
    .unwrap();
    e.commit().unwrap();
    assert_eq!(ids(e.search(&q).unwrap()), Vec::<u64>::new());
    assert_eq!(e.info().docs, 2);
}

#[test]
fn an_index_opens_again_and_a_reader_sees_commits_after_a_reload() {
    let dir = TempDir::new("reopen");
    {
        let e = Engine::open(&dir.0, 20_000_000).unwrap();
        fill(&e);
        e.apply(&Batch {
            add: vec![doc(9, BOOK_1, LIB_A, "en", "uncommitted words")],
            ..Default::default()
        })
        .unwrap();
    }
    let e = Engine::open(&dir.0, 20_000_000).unwrap();
    assert_eq!(e.info().docs, 5, "what was not committed is gone");
    let r = Engine::open(&dir.0, 0).unwrap();
    assert!(r.apply(&Batch::default()).is_err());
    e.apply(&Batch {
        add: vec![doc(10, BOOK_1, LIB_A, "en", "more words")],
        ..Default::default()
    })
    .unwrap();
    e.commit().unwrap();
    assert_eq!(r.info().docs, 5);
    r.reload().unwrap();
    assert_eq!(r.info().docs, 6);
}

#[test]
fn better_matches_score_higher() {
    let dir = TempDir::new("score");
    let e = Engine::open(&dir.0, 20_000_000).unwrap();
    e.apply(&Batch {
        add: vec![
            doc(
                1,
                BOOK_1,
                LIB_A,
                "en",
                "storm and sea and a long quiet afternoon of nothing much",
            ),
            doc(2, BOOK_2, LIB_A, "en", "storm storm storm"),
        ],
        ..Default::default()
    })
    .unwrap();
    e.commit().unwrap();
    let mut q = search(&[LIB_A]);
    q.words = "storm".into();
    let hits = e.search(&q).unwrap();
    assert_eq!(hits[0].id, 2);
    assert!(hits[0].score > hits[1].score);
}

#[test]
fn the_c_interface_round_trips() {
    let dir = TempDir::new("ffi");
    let path = CString::new(dir.0.to_str().unwrap()).unwrap();
    unsafe {
        let mut e: *mut Engine = std::ptr::null_mut();
        let mut errp: *mut c_char = std::ptr::null_mut();
        assert_eq!(gtf_open(path.as_ptr(), 20_000_000, &mut e, &mut errp), 0);
        let batch = CString::new(format!(
            r#"{{"add":[{{"id":7,"book":"{BOOK_1}","library":"{LIB_A}","lang":"en","text":"whales"}}]}}"#
        ))
        .unwrap();
        assert_eq!(gtf_apply(e, batch.as_ptr(), &mut errp), 0);
        assert_eq!(gtf_commit(e, &mut errp), 0);
        let q = CString::new(format!(
            r#"{{"words":"whale","libraries":["{LIB_A}"],"limit":10}}"#
        ))
        .unwrap();
        let mut out: *mut c_char = std::ptr::null_mut();
        assert_eq!(gtf_search(e, q.as_ptr(), &mut out, &mut errp), 0);
        let got = CStr::from_ptr(out).to_str().unwrap().to_string();
        gtf_free(out);
        assert!(got.starts_with(r#"[{"id":7,"score":"#), "{got}");

        let bad = CString::new("{").unwrap();
        assert_eq!(gtf_apply(e, bad.as_ptr(), &mut errp), 1);
        assert!(!errp.is_null());
        gtf_free(errp);
        gtf_close(e);
        assert!(CStr::from_ptr(gtf_version())
            .to_str()
            .unwrap()
            .starts_with("gotome-fulltext "));
    }
}

#[test]
fn snippets_mark_the_query_in_passages_it_is_given() {
    let dir = TempDir::new("snippets");
    let e = Engine::open(&dir.0, 20_000_000).unwrap();
    fill(&e);
    let mut q = search(&[LIB_A, LIB_B]);
    q.words = "whale run".into();
    let r = SnippetRequest {
        query: q,
        passages: vec![
            Passage { lang: "en".into(), text: "Long before, the whales were running before the harpooned ship.".into() },
            Passage { lang: "en".into(), text: "Nothing here.".into() },
        ],
        max_chars: 200,
    };
    let got = e.snippets(&r).unwrap();
    let marked: Vec<&str> = got[0].highlighted.iter().map(|&(s, e)| &got[0].fragment[s..e]).collect();
    assert_eq!(marked, vec!["whales", "running"]);
    assert!(got[1].highlighted.is_empty());
}
