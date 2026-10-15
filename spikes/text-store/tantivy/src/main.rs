//! Builds a Tantivy index of the chunks extract wrote, as GOtome would hold
//! it inside the app (#244): the three language fields with pg_search's
//! tokenizers, the book and the library as fields to filter on, and the text
//! in Tantivy's document store or not at all. Reports the index's files by
//! kind and times the searches of queries.json.
//!
//!     tantivy-spike <chunks.jsonl.gz> <index dir> <store> <queries.json> <result.json>
//!
//! store is `none`, `lz4`, or `zstd<level>[-<block KB>]`. Without a store,
//! snippets are made from the text handed in, read again from the chunks
//! for the hits, as the app would hand in text it keeps elsewhere (#268).
//! RUNS (default 5) and WINDOWS (default 1000, comma separated) set how
//! often each query runs and how many hits it takes.

use std::collections::{BTreeMap, HashMap, HashSet};
use std::fs::File;
use std::io::{BufRead, BufReader};
use std::time::Instant;

use flate2::read::GzDecoder;
use serde_json::{json, Value};
use tantivy::collector::TopDocs;
use tantivy::query::{BooleanQuery, Occur, PhraseQuery, Query, TermQuery};
use tantivy::schema::{
    Field, IndexRecordOption, Schema, TextFieldIndexing, TextOptions, FAST, INDEXED, STRING,
};
use tantivy::store::{Compressor, ZstdCompressor};
use tantivy::tokenizer::{
    Language, LowerCaser, RemoveLongFilter, SimpleTokenizer, Stemmer, TextAnalyzer,
};
use tantivy::{doc, Index, IndexSettings, IndexWriter, SnippetGenerator, TantivyDocument, Term};

const SNIPPET_HITS: usize = 20;
const SNIPPET_CHARS: usize = 200;
const LANGS: [&str; 3] = ["en", "de", "xx"];

type Res<T> = Result<T, Box<dyn std::error::Error>>;

fn main() -> Res<()> {
    let args: Vec<String> = std::env::args().collect();
    if args.len() != 6 {
        return Err("usage: tantivy-spike <chunks.jsonl.gz> <index dir> <store> <queries.json> <result.json>".into());
    }
    let (src, dir, store, queries, out) = (&args[1], &args[2], &args[3], &args[4], &args[5]);
    let stored = store != "none";
    let runs: usize = std::env::var("RUNS").map_or(Ok(5), |v| v.parse())?;
    let windows: Vec<usize> = std::env::var("WINDOWS")
        .unwrap_or_else(|_| "1000".into())
        .split(',')
        .map(|w| w.parse())
        .collect::<Result<_, _>>()?;
    let rss_start = rss();

    let mut sb = Schema::builder();
    let id = sb.add_u64_field("id", INDEXED | FAST);
    let book = sb.add_text_field("book_id", STRING | FAST);
    let library = sb.add_text_field("library_id", STRING | FAST);
    let mut bodies = Vec::new();
    for lang in LANGS {
        let mut opts = TextOptions::default().set_indexing_options(
            TextFieldIndexing::default()
                .set_tokenizer(lang)
                .set_index_option(IndexRecordOption::WithFreqsAndPositions),
        );
        if stored {
            opts = opts.set_stored();
        }
        bodies.push(sb.add_text_field(&format!("body_{lang}"), opts));
    }
    let schema = sb.build();

    let mut settings = IndexSettings::default();
    if let Some(rest) = store.strip_prefix("zstd") {
        let (level, block) = rest.split_once('-').unwrap_or((rest, "16"));
        settings.docstore_compression = Compressor::Zstd(ZstdCompressor {
            compression_level: Some(level.parse()?),
        });
        settings.docstore_blocksize = block.parse::<usize>()? * 1024;
    } else if store == "lz4" {
        settings.docstore_compression = Compressor::Lz4;
    }
    std::fs::create_dir_all(dir)?;
    let index = Index::builder()
        .schema(schema)
        .settings(settings)
        .create_in_dir(dir)?;
    for (lang, stem) in LANGS
        .iter()
        .zip([Some(Language::English), Some(Language::German), None])
    {
        let base = TextAnalyzer::builder(SimpleTokenizer::default())
            .filter(RemoveLongFilter::limit(255))
            .filter(LowerCaser);
        let analyzer = match stem {
            Some(l) => base.filter(Stemmer::new(l)).build(),
            None => base.build(),
        };
        index.tokenizers().register(lang, analyzer);
    }

    // The limits of #14's Postgres: four threads, well under 2 GB.
    let start = Instant::now();
    let mut writer: IndexWriter = index.writer_with_num_threads(4, 800_000_000)?;
    let (mut books, mut chunks, mut text_bytes) = (0u64, 0u64, 0u64);
    let reader = BufReader::with_capacity(1 << 20, GzDecoder::new(File::open(src)?));
    for line in reader.lines() {
        let b: Value = serde_json::from_str(&line?)?;
        books += 1;
        let book_id = format!("00000000-0000-4000-8000-{:012}", b["id"].as_u64().unwrap());
        let library_id = format!(
            "00000000-0000-4000-a000-{:012}",
            b["library"].as_u64().unwrap()
        );
        for c in b["chunks"].as_array().unwrap() {
            chunks += 1;
            let text = c["text"].as_str().unwrap();
            text_bytes += text.len() as u64;
            let body = match c["lang"].as_str().unwrap() {
                "en" => bodies[0],
                "de" => bodies[1],
                _ => bodies[2],
            };
            writer.add_document(doc!(id => chunks, book => book_id.as_str(), library => library_id.as_str(), body => text))?;
        }
    }
    writer.commit()?;
    let indexed = start.elapsed().as_secs_f64();
    let segments = index.searchable_segment_ids()?;
    let segment_count = segments.len();
    if segments.len() > 1 {
        writer.merge(&segments).wait()?;
    }
    writer.wait_merging_threads()?;
    let merged = start.elapsed().as_secs_f64();
    let writer: IndexWriter = index.writer_with_num_threads(1, 50_000_000)?;
    writer.garbage_collect_files().wait()?;
    drop(writer);
    let rss_built = rss();

    let mut sizes: BTreeMap<String, u64> = BTreeMap::new();
    for entry in std::fs::read_dir(dir)? {
        let path = entry?.path();
        let ext = path
            .extension()
            .and_then(|e| e.to_str())
            .unwrap_or("other")
            .to_string();
        *sizes.entry(ext).or_default() += std::fs::metadata(&path)?.len();
    }
    let total: u64 = sizes.values().sum();
    sizes.insert("total".into(), total);
    println!("{store}: {books} books, {chunks} chunks indexed in {indexed:.0} s ({segment_count} segments), merged by {merged:.0} s");
    for (k, v) in &sizes {
        println!("  {k:10} {:8.1} MB", *v as f64 / 1e6);
    }

    let reader = index.reader()?;
    let searcher = reader.searcher();
    let rss_opened = rss();
    let libs: Vec<Term> = (0..2)
        .map(|i| Term::from_field_text(library, &format!("00000000-0000-4000-a000-{i:012}")))
        .collect();
    let qs: Vec<Value> = serde_json::from_reader(File::open(queries)?)?;
    let mut latency = Vec::new();
    for &window in &windows {
        for q in &qs {
            let snippets = q["snippets"].as_bool().unwrap_or(false);
            let (text, phrase) = match q["phrase"].as_str() {
                Some(p) => (p, true),
                None => (q["words"].as_str().unwrap(), false),
            };
            let one_library = q["library"].as_bool().unwrap_or(false);
            let matcher = text_query(&index, &bodies, text, phrase)?;
            let lib_terms = if one_library { &libs[..1] } else { &libs[..] };
            let lib_query: Box<dyn Query> = Box::new(BooleanQuery::new(
                lib_terms
                    .iter()
                    .map(|t| {
                        (
                            Occur::Should,
                            Box::new(TermQuery::new(t.clone(), IndexRecordOption::Basic))
                                as Box<dyn Query>,
                        )
                    })
                    .collect(),
            ));
            let query = BooleanQuery::new(vec![
                (Occur::Must, matcher.box_clone()),
                (Occur::Must, lib_query),
            ]);
            let mut handed_in = HashMap::new();
            if snippets && !stored {
                let top = searcher.search(&query, &TopDocs::with_limit(window))?;
                let mut want = HashSet::new();
                for (_, addr) in top.iter().take(SNIPPET_HITS) {
                    want.insert(chunk_id(&searcher, *addr)?);
                }
                handed_in = read_texts(src, &want)?;
            }
            let mut times = Vec::new();
            let mut hits = 0;
            for run in 0..=runs {
                let start = Instant::now();
                let top = searcher.search(&query, &TopDocs::with_limit(window))?;
                hits = top.len();
                if snippets {
                    let mut gens = Vec::new();
                    for &f in &bodies {
                        let mut g = SnippetGenerator::create(&searcher, &*matcher, f)?;
                        g.set_max_num_chars(SNIPPET_CHARS);
                        gens.push(g);
                    }
                    for (_, addr) in top.iter().take(SNIPPET_HITS) {
                        if stored {
                            let d: TantivyDocument = searcher.doc(*addr)?;
                            for g in &gens {
                                if !g.snippet_from_doc(&d).is_empty() {
                                    break;
                                }
                            }
                        } else if let Some((lang, body)) =
                            handed_in.get(&chunk_id(&searcher, *addr)?)
                        {
                            let i = LANGS.iter().position(|l| l == lang).unwrap_or(2);
                            gens[i].snippet(body);
                        }
                    }
                }
                if run > 0 {
                    times.push(start.elapsed().as_secs_f64() * 1000.0);
                }
            }
            times.sort_by(|a, b| a.partial_cmp(b).unwrap());
            let p50 = times[times.len() / 2];
            let p95 = times[(times.len() * 95).div_ceil(100) - 1];
            let max = times[times.len() - 1];
            let kind = q["kind"].as_str().unwrap();
            println!(
                "  top {window:4} {kind:8} {:24} {hits:5} hits  p50 {p50:7.1} ms  p95 {p95:7.1} ms",
                format!("{text:?}")
            );
            latency.push(json!({"kind": kind, "query": text, "top": window, "hits": hits, "p50": p50, "p95": p95, "max": max}));
        }
    }
    let rss_searched = rss();

    let result = json!({
        "store": store, "books": books, "chunks": chunks, "textBytes": text_bytes,
        "segmentsBeforeMerge": segment_count, "indexSeconds": indexed, "mergedSeconds": merged,
        "sizes": sizes, "latencyMs": latency, "runs": runs,
        "rssKB": {"start": rss_start, "built": rss_built, "opened": rss_opened, "searched": rss_searched},
    });
    std::fs::write(out, serde_json::to_string_pretty(&result)? + "\n")?;
    Ok(())
}

/// The app's condition: every word in one field, or the phrase in one
/// field, of any of the three.
fn text_query(index: &Index, bodies: &[Field], text: &str, phrase: bool) -> Res<Box<dyn Query>> {
    let mut alts: Vec<(Occur, Box<dyn Query>)> = Vec::new();
    for &f in bodies {
        let mut analyzer = index.tokenizer_for_field(f)?;
        let mut terms = Vec::new();
        let mut stream = analyzer.token_stream(text);
        while let Some(t) = stream.next() {
            terms.push(Term::from_field_text(f, &t.text));
        }
        if terms.is_empty() {
            continue;
        }
        let q: Box<dyn Query> = if phrase && terms.len() > 1 {
            Box::new(PhraseQuery::new(terms))
        } else {
            Box::new(BooleanQuery::new(
                terms
                    .into_iter()
                    .map(|t| {
                        (
                            Occur::Must,
                            Box::new(TermQuery::new(t, IndexRecordOption::WithFreqs))
                                as Box<dyn Query>,
                        )
                    })
                    .collect(),
            ))
        };
        alts.push((Occur::Should, q));
    }
    Ok(Box::new(BooleanQuery::new(alts)))
}

fn chunk_id(searcher: &tantivy::Searcher, addr: tantivy::DocAddress) -> Res<u64> {
    let ids = searcher
        .segment_reader(addr.segment_ord)
        .fast_fields()
        .u64("id")?;
    Ok(ids.first(addr.doc_id).ok_or("chunk without id")?)
}

/// The language and text of the chunks in want, numbered as they were
/// indexed: what the app would hand in from wherever it keeps the text.
fn read_texts(src: &str, want: &HashSet<u64>) -> Res<HashMap<u64, (String, String)>> {
    let mut out = HashMap::new();
    let mut n = 0u64;
    let reader = BufReader::with_capacity(1 << 20, GzDecoder::new(File::open(src)?));
    for line in reader.lines() {
        let b: Value = serde_json::from_str(&line?)?;
        for c in b["chunks"].as_array().unwrap() {
            n += 1;
            if want.contains(&n) {
                out.insert(
                    n,
                    (
                        c["lang"].as_str().unwrap().to_string(),
                        c["text"].as_str().unwrap().to_string(),
                    ),
                );
            }
        }
    }
    Ok(out)
}

/// The process's resident and peak memory in KB, from /proc/self/status.
fn rss() -> Value {
    let status = std::fs::read_to_string("/proc/self/status").unwrap_or_default();
    let mut v = serde_json::Map::new();
    for line in status.lines() {
        for key in ["VmRSS", "VmHWM", "RssAnon", "RssFile"] {
            if let Some(rest) = line.strip_prefix(&format!("{key}:")) {
                let kb: u64 = rest
                    .trim()
                    .trim_end_matches("kB")
                    .trim()
                    .parse()
                    .unwrap_or(0);
                v.insert(key.into(), kb.into());
            }
        }
    }
    Value::Object(v)
}
