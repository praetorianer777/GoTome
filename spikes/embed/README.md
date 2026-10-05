# Embedding runtime benchmark

The harness behind `docs/decisions/embedding-runtime.md` (#15). It runs
`intfloat/multilingual-e5-small` three ways: through [hugot](https://github.com/knights-analytics/hugot)
in its two backends, pure Go (GoMLX) and ONNX Runtime, and on ONNX Runtime
directly with a tokenizer of its own (`direct.go`, `unigram.go`), each with
the model's fp32 and int8 weights, and measures them on the benchmark corpus.
Nothing here runs in the gate.

```bash
make embed-assets                      # owner: model and ONNX Runtime, about 640 MB
make embed-reference                   # owner: reference vectors with sentence-transformers
make search-spike-extract              # the corpus as chunks, shared with the search spike
make embed-spike STEP=check            # tokens and vectors against the reference
make embed-spike STEP=speed            # passages per second and memory
make embed-spike STEP=books            # every book's sampled chunks (BACKEND, WEIGHTS, SAMPLES, PREFIX)
make embed-spike STEP=quality          # related books found by the book vectors
make embed-spike-test                  # the harness's own tests
```

## Downloads

`fetch.sh` takes the model at a fixed revision of its repository and checks
each large file against the SHA-256 Hugging Face publishes for it; ONNX
Runtime 1.30.0, the version hugot 0.8.1 is built against, is checked against
GitHub's digest of each release file. Both go to `.cache/embed/`.

`reference.py` embeds `testdata/texts.json` with sentence-transformers on
PyTorch, the model's own implementation, and writes token IDs and vectors to
`testdata/reference.json`. The texts are queries and passages in English,
German and French, special characters, an empty query, and passages longer
than the model's 512 tokens.

## What it measures

- **check**: the token IDs of hugot's pure-Go tokenizer, and of the Viterbi
  Unigram tokenizer the direct backend uses, against the reference's, and the cosine of each vector to the reference vector, for
  both backends and both weights. hugot cuts texts to the model's
  `max_position_embeddings`, which is 514 for this model while it takes 512
  tokens; the harness cuts to 512, and one run shows what the default does.
- **speed**: 128 chunks of the corpus (`SPEED_PASSAGES`), round-robin over
  the books so that all three languages are in it, each cut to 512 tokens
  and prefixed with `passage: `, in batches of 8 after one batch to warm up;
  `BACKENDS` picks which of `go ort direct` run. It reports
  passages per second, the highest anonymous memory of the container, the
  size of the weights, of the executable and of the ONNX Runtime library.
- **books**: for every book, up to `SAMPLES` chunks (64) spread evenly over
  it, each embedded with `PREFIX` (`passage: `) before it. 32 and 16 samples are every second and every fourth
  of these, so one run serves all three counts.
- **quality**: a book's vector is the normalised mean of its samples'.
  For every book, the others are ranked by cosine, and the harness reports
  where the nearest related book stands: another volume of the same work
  (the title without "Band", "Vol.", "(of 3)" and the like, by the same
  authors), and another book by the same author. It also reports how many of
  the ten nearest books share a subject heading, against the share among all
  pairs, and how many are in the same language.

## Limits

Each run gets a container with 2 GB of memory, no swap and four CPUs, like
the search spike's engines. The host is a twelve-core x86-64 machine doing
other work, so rates are comparable with each other and not exact. There is
no arm64 machine here: the aarch64 build of ONNX Runtime is downloaded for
#64 but not measured.
