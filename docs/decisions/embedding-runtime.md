# Embedding runtime

Decided in #15, 2026-10-05.

## Question

GOtome suggests similar books from a vector per book: the normalised mean of
the vectors of passages sampled from its text, made with
`intfloat/multilingual-e5-small` (384 dimensions, 512 tokens, English,
German, French and more). The model runs inside the app's process, as a
background job on hardware like a NAS, within a container's memory. Two
things were open: which runtime runs it (hugot's pure-Go backend, hugot on
ONNX Runtime, or ONNX Runtime directly), and how much it can afford: which
weights, and how many passages per book.

## Decision

GOtome runs the model on **ONNX Runtime 1.30 directly**, through its own Go
binding (`github.com/microsoft/onnxruntime/go/onnxruntime`, cgo, the library
loaded at start), with **its own tokenizer**: a Viterbi segmentation over the
model's SentencePiece Unigram vocabulary, as Hugging Face's `tokenizers` does
(`spikes/embed/unigram.go`). Not through hugot.

- **Weights: int8** (`onnx/model_qint8_avx512_vnni.onnx` of the model's
  repository, 118 MB). The fp32 weights (470 MB) are no better at finding
  related books, need about 350 MB more memory, and are only 10–20 % faster
  on a CPU without int8 dot-product instructions.
- **16 passages per book**, evenly spread over its chunks, each cut to 512
  tokens. 32 find other volumes of a work slightly more often and nothing
  else better, at twice the cost.
- **Prefix `query: `** for every passage. A book is compared with books,
  which the model's authors call a symmetric task, and the prefix found
  volumes and authors a little better than `passage: ` did.
- **The model is downloaded on first use** into the data volume, at a fixed
  revision, each file checked against its published SHA-256, as
  `spikes/embed/fetch.sh` does. It is not in the image. The ONNX Runtime
  library (29 MB) is: the app image is built with cgo from #64 on, for amd64
  and arm64.
- Vectors are stored with the model's name, revision and weights, so that a
  change of any re-embeds (#65).

## Measurements

The corpus of the search spike: 2,011 public-domain books in English, German
and French, cut into chunks as #53 cuts them. Every run had a container of its
own with 2 GB of memory, no swap and four CPUs of a Ryzen 5 3600 (AVX2, no
AVX-512 or VNNI), batches of 8 passages of 512 tokens.

Agreement with the model's own implementation (sentence-transformers on
PyTorch), on 14 texts in three languages, with special characters, an empty
query and passages longer than 512 tokens:

| Runtime | Weights | Token IDs equal | Lowest cosine to the reference |
|---|---|---|---|
| ONNX Runtime directly, own tokenizer | fp32 | 14 of 14 | 1.000 |
| ONNX Runtime directly, own tokenizer | int8 | 14 of 14 | 0.982 |
| hugot on ONNX Runtime | fp32 | 2 of 14 | 0.890 |
| hugot on ONNX Runtime | int8 | 2 of 14 | 0.889 |

Speed and memory (two runs each, after the books had been embedded; the host
was otherwise quiet):

| Runtime | Weights | Passages per second | Memory at peak |
|---|---|---|---|
| ONNX Runtime directly | fp32 | 6.3–7.0 | 1.07 GB |
| ONNX Runtime directly | int8 | 5.6–5.8 | 0.70–0.72 GB |
| hugot on ONNX Runtime | fp32 | 5.6–6.4 | 1.08 GB |
| hugot on ONNX Runtime | int8 | 5.2–5.7 | 0.74–0.76 GB |
| hugot pure Go | fp32 | 0.17 | 2.58 GB (8 GB limit) |
| hugot pure Go | int8 | 0.04 | 1.50 GB |

Related books found by the book vectors (`quality`): for each book, where the
nearest related book stands among all others, as mean reciprocal rank;
"volumes" are other volumes of the same work (134 books), "authors" other
books by the same author (861 books). Subject precision is how many of the
ten nearest share a subject heading, against 0.015 for two books at random.

| Run | Samples | Volumes MRR (top 1) | Authors MRR | Subject precision | Same language |
|---|---|---|---|---|---|
| direct, int8, `passage:` | 16 | 0.938 (90 %) | 0.425 | 0.174 | 0.990 |
| direct, fp32, `query:` | 16 | 0.926 (89 %) | 0.418 | 0.168 | 0.988 |
| direct, fp32, `passage:` | 16 | 0.920 (88 %) | 0.409 | 0.168 | 0.989 |
| direct, fp32, `passage:` | 32 | 0.939 (90 %) | 0.402 | 0.169 | 0.989 |
| direct, fp32, `passage:` | 64 | 0.944 (91 %) | 0.408 | 0.167 | 0.989 |
| hugot, fp32, `passage:` | 16 | 0.923 (89 %) | 0.397 | 0.165 | 0.988 |

Embedding a book with 16 passages takes about 3 seconds at these rates; the
2,011 books of the corpus took 3 hours 13 minutes with up to 64. A library of
50,000 books is 800,000 passages: about 40 hours at 5.6 passages per second
on four cores of this machine, and several times that on a NAS's slower
cores.

## Reasons

- **The pure-Go backend is out.** Forty times slower than ONNX Runtime, and
  with fp32 weights more than 2 GB of memory: 50,000 books would take
  months.
- **hugot's tokenizer gives other tokens than the model's.** Its pure-Go
  tokenizer (go-huggingface) takes the longest matching piece at each step
  instead of the most probable segmentation the Unigram model defines, and
  does not cut at 512 tokens, which made the model fail on a long passage.
  Vectors then differ from the model's own (cosine 0.89 at worst), and from
  any other tool's that uses the model correctly. The book vectors were
  still about as good at finding related books, as averaging hides much,
  but a query or a text embedded elsewhere would not match them. Fixing
  the tokenizer inside hugot was not ours to do; writing it was under 200
  lines with tests against the reference, and hugot's other parts are not
  needed once the tokenizer is ours. The difference is worth reporting to
  hugot.
- **int8 over fp32.** Book vectors from int8 weights were no worse (better,
  within noise) at every measure, and a third less memory and a quarter of
  the download matter more on a NAS than the 10–20 % of speed fp32 has
  here. CPUs with VNNI or Arm's dot-product instructions run int8 faster
  than fp32, which is where int8 should gain.
- **16 samples.** Between 16 and 64 samples, finding other volumes of a work
  rose from 0.920 to 0.944 and nothing else changed; four times the cost for
  that is not worth it while a first run over a library takes days.
- **The prefix.** With the same weights, `query:` found other volumes and
  other books by the author a little more often than `passage:` (0.926
  against 0.920, 0.418 against 0.409) and did as well on subjects and
  language; small, and it is what the model's authors advise for comparing
  like with like.

## Limits

- **Not measured on arm64.** There is no arm64 machine here. ONNX Runtime
  publishes an aarch64 build of the same version, which #64 puts into the
  arm64 image; the first arm64 deployment should run `make embed-spike
  STEP=speed` once.
- **The rates are of one machine**, with its other work stopped for the speed
  runs. They compare the runtimes; a NAS is slower by a factor no run here
  measured. Embedding is therefore the lowest-priority background job,
  resumable, and can be switched off (#65).
- **The quality runs differ in noise.** The int8 run was best by a little, and
  each run sampled the same chunks; differences of 0.01 between runs are not
  findings. What they show is that no choice here costs quality.
- **cgo.** The app was a static Go binary; it now needs glibc and the ONNX
  Runtime library beside it, in both images. Tests that load the model run
  only where the model has been downloaded (#64 decides how the gate stays
  without it).
