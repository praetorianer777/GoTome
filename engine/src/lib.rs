//! GOtome's full-text index (#265, docs/decisions/text-store.md): Tantivy in
//! the app's process, behind a C interface that `backend/internal/fulltext`
//! loads at run time. The index holds no text: per chunk its ID, its book,
//! its library and its words, in the field of its language.
//!
//! Requests and answers cross the interface as JSON, which keeps it to a few
//! functions of plain C types. Every function returns 0 or, on failure, a
//! nonzero value with the message in `*err`, which the caller frees with
//! `gtf_free`; a panic is caught and answered the same way.

use std::ffi::{c_char, c_int, CStr, CString};
use std::panic::{catch_unwind, AssertUnwindSafe};
use std::path::Path;
use std::sync::Mutex;

use serde::{Deserialize, Serialize};
use tantivy::collector::TopDocs;
use tantivy::directory::MmapDirectory;
use tantivy::query::{
    BooleanQuery, EmptyQuery, Occur, PhraseQuery, Query, TermQuery, TermSetQuery,
};
use tantivy::schema::{
    Field, IndexRecordOption, Schema, TextFieldIndexing, TextOptions, FAST, INDEXED, STRING,
};
use tantivy::tokenizer::{
    Language, LowerCaser, RemoveLongFilter, SimpleTokenizer, Stemmer, TextAnalyzer,
};
use tantivy::{Index, IndexReader, IndexWriter, ReloadPolicy, TantivyDocument, Term};

/// What an index written by this library is: a change to the schema or the
/// tokenizers raises it, and an index of another one is built again.
pub const FORMAT: u32 = 1;

const VERSION: &str = concat!(
    "gotome-fulltext ",
    env!("CARGO_PKG_VERSION"),
    ", tantivy 0.22.1, format 1\0"
);

/// The language fields and their tokenizers: pg_search's `pdb.simple` with
/// the English and the German Snowball stemmer, and without one, as
/// `book_chunks_bm25` indexes `body_en`, `body_de` and `body_xx`.
const LANGS: [(&str, Option<Language>); 3] = [
    ("en", Some(Language::English)),
    ("de", Some(Language::German)),
    ("xx", None),
];

/// Tantivy refuses a writer with less per thread.
const MIN_HEAP: u64 = 15_000_000;

pub struct Engine {
    index: Index,
    reader: IndexReader,
    writer: Option<Mutex<IndexWriter>>,
    id: Field,
    book: Field,
    library: Field,
    bodies: [Field; 3],
}

fn schema() -> Schema {
    let mut sb = Schema::builder();
    sb.add_u64_field("id", INDEXED | FAST);
    sb.add_text_field("book", STRING);
    sb.add_text_field("library", STRING);
    for (lang, _) in LANGS {
        sb.add_text_field(
            &format!("body_{lang}"),
            TextOptions::default().set_indexing_options(
                TextFieldIndexing::default()
                    .set_tokenizer(&format!("gotome_{lang}"))
                    .set_index_option(IndexRecordOption::WithFreqsAndPositions),
            ),
        );
    }
    sb.build()
}

fn analyzer(stem: Option<Language>) -> TextAnalyzer {
    let base = TextAnalyzer::builder(SimpleTokenizer::default())
        .filter(RemoveLongFilter::limit(255))
        .filter(LowerCaser);
    match stem {
        Some(l) => base.filter(Stemmer::new(l)).build(),
        None => base.build(),
    }
}

type Res<T> = Result<T, String>;

fn err<E: std::fmt::Display>(e: E) -> String {
    e.to_string()
}

impl Engine {
    /// Opens the index in dir, making it when the folder holds none. A heap
    /// of 0 opens it for searching only; otherwise the one writer of the
    /// index is taken, with that budget for its buffer.
    pub fn open(dir: &Path, heap: u64) -> Res<Engine> {
        std::fs::create_dir_all(dir).map_err(err)?;
        let directory = MmapDirectory::open(dir).map_err(err)?;
        let want = schema();
        let index = if Index::exists(&directory).map_err(err)? {
            let index = Index::open(directory).map_err(err)?;
            if index.schema() != want {
                return Err(format!("the index in {} has another schema", dir.display()));
            }
            index
        } else {
            Index::create(directory, want, Default::default()).map_err(err)?
        };
        for (lang, stem) in LANGS {
            index
                .tokenizers()
                .register(&format!("gotome_{lang}"), analyzer(stem));
        }
        let reader = index
            .reader_builder()
            .reload_policy(ReloadPolicy::Manual)
            .try_into()
            .map_err(err)?;
        let writer = if heap == 0 {
            None
        } else {
            let w: IndexWriter = index
                .writer_with_num_threads(1, heap.max(MIN_HEAP) as usize)
                .map_err(err)?;
            Some(Mutex::new(w))
        };
        let s = index.schema();
        let field = |name: &str| s.get_field(name).map_err(err);
        Ok(Engine {
            id: field("id")?,
            book: field("book")?,
            library: field("library")?,
            bodies: [field("body_en")?, field("body_de")?, field("body_xx")?],
            index,
            reader,
            writer,
        })
    }

    fn writer(&self) -> Res<std::sync::MutexGuard<'_, IndexWriter>> {
        match &self.writer {
            Some(w) => w
                .lock()
                .map_err(|_| "the writer panicked before".to_string()),
            None => Err("the index is open for searching only".into()),
        }
    }

    /// Applies the batch's deletes, then its documents, to the writer. Its
    /// documents are found after the next commit; a document added again
    /// under an ID it had is added again, so a batch deletes first what it
    /// adds.
    pub fn apply(&self, b: &Batch) -> Res<()> {
        let w = self.writer()?;
        for &id in &b.delete_chunks {
            w.delete_term(Term::from_field_u64(self.id, id));
        }
        for book in &b.delete_books {
            w.delete_term(Term::from_field_text(self.book, book));
        }
        for lib in &b.delete_libraries {
            w.delete_term(Term::from_field_text(self.library, lib));
        }
        for d in &b.add {
            let body = match d.lang.as_str() {
                "en" => self.bodies[0],
                "de" => self.bodies[1],
                _ => self.bodies[2],
            };
            let mut doc = TantivyDocument::default();
            doc.add_u64(self.id, d.id);
            doc.add_text(self.book, &d.book);
            doc.add_text(self.library, &d.library);
            doc.add_text(body, &d.text);
            w.add_document(doc).map_err(err)?;
        }
        Ok(())
    }

    /// Makes what was applied durable and searchable.
    pub fn commit(&self) -> Res<()> {
        let mut w = self.writer()?;
        w.commit().map_err(err)?;
        drop(w);
        self.reader.reload().map_err(err)
    }

    /// Looks again for what another process committed; a writer's own
    /// commits are seen at once.
    pub fn reload(&self) -> Res<()> {
        self.reader.reload().map_err(err)
    }

    pub fn info(&self) -> Info {
        let s = self.reader.searcher();
        Info {
            docs: s.num_docs(),
            segments: s.segment_readers().len() as u64,
        }
    }

    /// The best chunks that match the query in one of the language fields,
    /// among the libraries and, when named, the books.
    pub fn search(&self, q: &Search) -> Res<Vec<Hit>> {
        if q.libraries.is_empty() || q.books.as_ref().is_some_and(|b| b.is_empty()) || q.limit == 0
        {
            return Ok(Vec::new());
        }
        let mut must: Vec<(Occur, Box<dyn Query>)> = vec![(Occur::Must, self.matcher(q)?)];
        must.push((Occur::Must, set(self.library, &q.libraries)));
        if let Some(books) = &q.books {
            must.push((Occur::Must, set(self.book, books)));
        }
        let query = BooleanQuery::new(must);
        let searcher = self.reader.searcher();
        let top = searcher
            .search(&query, &TopDocs::with_limit(q.limit))
            .map_err(err)?;
        let mut out = Vec::with_capacity(top.len());
        for (score, addr) in top {
            let ids = searcher
                .segment_reader(addr.segment_ord)
                .fast_fields()
                .u64("id")
                .map_err(err)?;
            if let Some(id) = ids.first(addr.doc_id) {
                out.push(Hit { id, score });
            }
        }
        Ok(out)
    }

    /// pg_search's condition, as `internal/search` writes it: in one of the
    /// language fields, every free word (`&&&`), one word of each group
    /// (`|||`), each phrase (`###`) and one phrase of each set of variants,
    /// each tokenized by that field's tokenizer.
    fn matcher(&self, q: &Search) -> Res<Box<dyn Query>> {
        let mut alts: Vec<(Occur, Box<dyn Query>)> = Vec::new();
        'field: for &f in &self.bodies {
            let mut tk = self.index.tokenizer_for_field(f).map_err(err)?;
            let mut terms = |text: &str| -> Vec<Term> {
                let mut out = Vec::new();
                let mut stream = tk.token_stream(text);
                while let Some(t) = stream.next() {
                    out.push(Term::from_field_text(f, &t.text));
                }
                out
            };
            let mut conds: Vec<(Occur, Box<dyn Query>)> = Vec::new();
            if !q.words.is_empty() {
                let ts = terms(&q.words);
                if ts.is_empty() {
                    continue 'field;
                }
                for t in ts {
                    conds.push((Occur::Must, term(t)));
                }
            }
            for g in &q.groups {
                let ts = terms(&g.join(" "));
                if ts.is_empty() {
                    continue 'field;
                }
                conds.push((Occur::Must, any(ts.into_iter().map(term).collect())));
            }
            for p in &q.phrases {
                match phrase(terms(p)) {
                    Some(pq) => conds.push((Occur::Must, pq)),
                    None => continue 'field,
                }
            }
            for vs in &q.variants {
                let either: Vec<Box<dyn Query>> =
                    vs.iter().filter_map(|v| phrase(terms(v))).collect();
                if either.is_empty() {
                    continue 'field;
                }
                conds.push((Occur::Must, any(either)));
            }
            if !conds.is_empty() {
                alts.push((Occur::Should, Box::new(BooleanQuery::new(conds))));
            }
        }
        if alts.is_empty() {
            return Ok(Box::new(EmptyQuery));
        }
        Ok(Box::new(BooleanQuery::new(alts)))
    }
}

fn term(t: Term) -> Box<dyn Query> {
    Box::new(TermQuery::new(t, IndexRecordOption::WithFreqs))
}

fn any(qs: Vec<Box<dyn Query>>) -> Box<dyn Query> {
    Box::new(BooleanQuery::new(
        qs.into_iter().map(|q| (Occur::Should, q)).collect(),
    ))
}

fn phrase(mut ts: Vec<Term>) -> Option<Box<dyn Query>> {
    match ts.len() {
        0 => None,
        1 => Some(term(ts.remove(0))),
        _ => Some(Box::new(PhraseQuery::new(ts))),
    }
}

/// A filter on a raw field, scoring nothing.
fn set(f: Field, values: &[String]) -> Box<dyn Query> {
    Box::new(TermSetQuery::new(
        values.iter().map(|v| Term::from_field_text(f, v)),
    ))
}

/// Changes to the index: chunks, books and libraries to delete, then chunks
/// to add.
#[derive(Deserialize, Default)]
#[serde(default)]
pub struct Batch {
    pub delete_chunks: Vec<u64>,
    pub delete_books: Vec<String>,
    pub delete_libraries: Vec<String>,
    pub add: Vec<Doc>,
}

#[derive(Deserialize)]
pub struct Doc {
    pub id: u64,
    pub book: String,
    pub library: String,
    /// "en", "de", or anything else for the field without a stemmer.
    pub lang: String,
    pub text: String,
}

#[derive(Deserialize, Default)]
#[serde(default)]
pub struct Search {
    pub words: String,
    pub groups: Vec<Vec<String>>,
    pub phrases: Vec<String>,
    pub variants: Vec<Vec<String>>,
    pub libraries: Vec<String>,
    pub books: Option<Vec<String>>,
    pub limit: usize,
}

#[derive(Serialize, Debug, PartialEq)]
pub struct Hit {
    pub id: u64,
    pub score: f32,
}

#[derive(Serialize)]
pub struct Info {
    pub docs: u64,
    pub segments: u64,
}

fn guard<F: FnOnce() -> Res<()>>(errp: *mut *mut c_char, f: F) -> c_int {
    let out = match catch_unwind(AssertUnwindSafe(f)) {
        Ok(Ok(())) => return 0,
        Ok(Err(e)) => e,
        Err(p) => match p.downcast_ref::<&str>() {
            Some(s) => format!("panic: {s}"),
            None => match p.downcast_ref::<String>() {
                Some(s) => format!("panic: {s}"),
                None => "panic".into(),
            },
        },
    };
    if !errp.is_null() {
        unsafe { *errp = to_c(out) };
    }
    1
}

fn to_c(s: String) -> *mut c_char {
    CString::new(s.replace('\0', " "))
        .unwrap_or_default()
        .into_raw()
}

unsafe fn from_c<'a>(s: *const c_char) -> Res<&'a str> {
    if s.is_null() {
        return Err("null string".into());
    }
    CStr::from_ptr(s).to_str().map_err(err)
}

unsafe fn engine<'a>(e: *const Engine) -> Res<&'a Engine> {
    e.as_ref().ok_or_else(|| "no index".to_string())
}

/// The library's version, a static string.
#[no_mangle]
pub extern "C" fn gtf_version() -> *const c_char {
    VERSION.as_ptr() as *const c_char
}

/// Opens or makes the index in dir; heap 0 opens it for searching only.
///
/// # Safety
/// dir is a NUL-terminated string; out and err point to writable pointers.
#[no_mangle]
pub unsafe extern "C" fn gtf_open(
    dir: *const c_char,
    heap: u64,
    out: *mut *mut Engine,
    errp: *mut *mut c_char,
) -> c_int {
    guard(errp, || {
        let e = Engine::open(Path::new(from_c(dir)?), heap)?;
        *out = Box::into_raw(Box::new(e));
        Ok(())
    })
}

/// Closes the index; what was applied and not committed is dropped.
///
/// # Safety
/// e is an index gtf_open returned, closed once, and in use by no other call.
#[no_mangle]
pub unsafe extern "C" fn gtf_close(e: *mut Engine) {
    if !e.is_null() {
        let _ = catch_unwind(AssertUnwindSafe(|| drop(Box::from_raw(e))));
    }
}

/// Applies a batch, as JSON.
///
/// # Safety
/// e is an open index; batch is a NUL-terminated string.
#[no_mangle]
pub unsafe extern "C" fn gtf_apply(
    e: *const Engine,
    batch: *const c_char,
    errp: *mut *mut c_char,
) -> c_int {
    guard(errp, || {
        let b: Batch = serde_json::from_str(from_c(batch)?).map_err(err)?;
        engine(e)?.apply(&b)
    })
}

/// # Safety
/// e is an open index.
#[no_mangle]
pub unsafe extern "C" fn gtf_commit(e: *const Engine, errp: *mut *mut c_char) -> c_int {
    guard(errp, || engine(e)?.commit())
}

/// # Safety
/// e is an open index.
#[no_mangle]
pub unsafe extern "C" fn gtf_reload(e: *const Engine, errp: *mut *mut c_char) -> c_int {
    guard(errp, || engine(e)?.reload())
}

/// Searches; the answer is a JSON array of hits, best first, in `*out`.
///
/// # Safety
/// e is an open index; query is a NUL-terminated string; out and err point
/// to writable pointers.
#[no_mangle]
pub unsafe extern "C" fn gtf_search(
    e: *const Engine,
    query: *const c_char,
    out: *mut *mut c_char,
    errp: *mut *mut c_char,
) -> c_int {
    guard(errp, || {
        let q: Search = serde_json::from_str(from_c(query)?).map_err(err)?;
        let hits = engine(e)?.search(&q)?;
        *out = to_c(serde_json::to_string(&hits).map_err(err)?);
        Ok(())
    })
}

/// What the index holds, as JSON.
///
/// # Safety
/// e is an open index; out and err point to writable pointers.
#[no_mangle]
pub unsafe extern "C" fn gtf_info(
    e: *const Engine,
    out: *mut *mut c_char,
    errp: *mut *mut c_char,
) -> c_int {
    guard(errp, || {
        let info = engine(e)?.info();
        *out = to_c(serde_json::to_string(&info).map_err(err)?);
        Ok(())
    })
}

/// Frees a string this library returned.
///
/// # Safety
/// s is a string from this library, freed once, or null.
#[no_mangle]
pub unsafe extern "C" fn gtf_free(s: *mut c_char) {
    if !s.is_null() {
        drop(CString::from_raw(s));
    }
}

#[cfg(test)]
mod tests;
