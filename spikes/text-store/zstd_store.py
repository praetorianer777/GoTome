"""What the chunks would take in a zstd store of GOtome's own (#244).

Reads the chunks extract wrote and compresses them with zstd in the ways
such a store could keep them: each chunk alone, each chunk with a dictionary
trained on chunks of other books, a book's chunks together, and blocks of a
fixed size across chunks as Tantivy's document store cuts them. Writes the
sizes, and how long reading one chunk back takes, as JSON; no text.

    python3 -I zstd_store.py <chunks.jsonl.gz> <result.json>
"""

import gzip
import json
import multiprocessing
import sys
import time

from compression import zstd

LEVELS = (3, 19)
BLOCKS = (16 * 1024, 128 * 1024)
DICT_SIZES = (112 * 1024, 1024 * 1024)
# A tenth of the books, by ID, train the dictionaries; the sizes with a
# dictionary are reported for the other books too, so that a dictionary
# does not look better for having seen the text.
TRAIN_EVERY = 10
TRAIN_CHUNKS_PER_BOOK = 20

_dicts = {}


def books(path):
    with gzip.open(path, "rt", encoding="utf-8") as f:
        for line in f:
            b = json.loads(line)
            yield b["id"], [c["text"].encode("utf-8") for c in b["chunks"]]


def blocks(chunks, size):
    """Cuts the chunks into blocks that end at the first chunk boundary at
    or after size bytes, as Tantivy's store does."""
    cur = []
    n = 0
    for c in chunks:
        cur.append(c)
        n += len(c)
        if n >= size:
            yield b"".join(cur)
            cur, n = [], 0
    if cur:
        yield b"".join(cur)


def init(dicts):
    for k, raw in dicts.items():
        _dicts[k] = zstd.ZstdDict(raw).as_digested_dict


def measure(item):
    book_id, chunks = item
    out = {
        "raw": sum(len(c) for c in chunks),
        "chunks": len(chunks),
        "heldOut": book_id % TRAIN_EVERY != 0,
    }
    sizes = {}
    seconds = {}
    for level in LEVELS:
        t = time.perf_counter()
        sizes[f"chunk-{level}"] = sum(len(zstd.compress(c, level)) for c in chunks)
        seconds[f"chunk-{level}"] = time.perf_counter() - t
        for k, d in _dicts.items():
            t = time.perf_counter()
            sizes[f"chunk-dict{k // 1024}k-{level}"] = sum(
                len(zstd.compress(c, level, zstd_dict=d)) for c in chunks
            )
            seconds[f"chunk-dict{k // 1024}k-{level}"] = time.perf_counter() - t
        t = time.perf_counter()
        whole = zstd.compress(b"".join(chunks), level)
        sizes[f"book-{level}"] = len(whole)
        seconds[f"book-{level}"] = time.perf_counter() - t
        for size in BLOCKS:
            t = time.perf_counter()
            sizes[f"block{size // 1024}k-{level}"] = sum(
                len(zstd.compress(b, level)) for b in blocks(chunks, size)
            )
            seconds[f"block{size // 1024}k-{level}"] = time.perf_counter() - t
    # Reading one chunk back: the chunk alone, or the book or the block it
    # is in, of the first chunk's compressed forms at level 19.
    first = chunks[0]
    reads = {}
    one = zstd.compress(first, 19)
    t = time.perf_counter()
    zstd.decompress(one)
    reads["chunk"] = time.perf_counter() - t
    if _dicts:
        k, d = next(iter(_dicts.items()))
        one = zstd.compress(first, 19, zstd_dict=d)
        t = time.perf_counter()
        zstd.decompress(one, zstd_dict=d)
        reads["chunk-dict"] = time.perf_counter() - t
    whole = zstd.compress(b"".join(chunks), 19)
    t = time.perf_counter()
    zstd.decompress(whole)
    reads["book"] = time.perf_counter() - t
    for size in BLOCKS:
        b = zstd.compress(next(blocks(chunks, size)), 19)
        t = time.perf_counter()
        zstd.decompress(b)
        reads[f"block{size // 1024}k"] = time.perf_counter() - t
    out["sizes"] = sizes
    out["seconds"] = seconds
    out["reads"] = reads
    return out


def main(src, dst):
    samples = []
    for book_id, chunks in books(src):
        if book_id % TRAIN_EVERY == 0:
            step = max(1, len(chunks) // TRAIN_CHUNKS_PER_BOOK)
            samples.extend(chunks[::step])
    t = time.perf_counter()
    dicts = {size: zstd.train_dict(samples, size).dict_content for size in DICT_SIZES}
    train_seconds = time.perf_counter() - t
    print(
        f"{len(samples)} chunks ({sum(map(len, samples)) / 1e6:.1f} MB) trained the dictionaries in {train_seconds:.0f} s"
    )

    with multiprocessing.Pool(initializer=init, initargs=(dicts,)) as pool:
        results = pool.map(measure, books(src), chunksize=4)

    def total(rows, key):
        return sum(r["sizes"][key] for r in rows)

    raw = sum(r["raw"] for r in results)
    held = [r for r in results if r["heldOut"]]
    held_raw = sum(r["raw"] for r in held)
    keys = sorted(results[0]["sizes"])
    report = {
        "books": len(results),
        "chunks": sum(r["chunks"] for r in results),
        "textBytes": raw,
        "trainSeconds": train_seconds,
        "dictionaryBytes": {f"{k // 1024}k": len(v) for k, v in dicts.items()},
        "variants": {},
        "readMicroseconds": {},
    }
    for k in keys:
        size = total(results, k)
        cpu = sum(r["seconds"][k] for r in results)
        report["variants"][k] = {
            "bytes": size,
            "ratio": size / raw,
            "heldOutRatio": total(held, k) / held_raw,
            "compressMBperCPUs": raw / 1e6 / cpu,
        }
        print(
            f"  {k:24} {size / 1e6:8.1f} MB  {size / raw:.3f} of the text  (held out {total(held, k) / held_raw:.3f})  {raw / 1e6 / cpu:6.1f} MB/s"
        )
    for k in results[0]["reads"]:
        times = sorted(r["reads"][k] for r in results)
        report["readMicroseconds"][k] = {
            "p50": times[len(times) // 2] * 1e6,
            "p95": times[int(len(times) * 0.95)] * 1e6,
        }
        print(
            f"  read one chunk from {k:10} p50 {report['readMicroseconds'][k]['p50']:8.0f} us"
        )
    with open(dst, "w") as f:
        json.dump(report, f, indent=2)
        f.write("\n")


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
