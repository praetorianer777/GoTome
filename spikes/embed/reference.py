"""Reference vectors for the embedding spike (#15).

Embeds testdata/texts.json with the model's own implementation,
sentence-transformers on PyTorch, at the revision fetch.sh pins, and writes
the token IDs and the normalised vectors to testdata/reference.json. The Go
harness checks both of its backends against them.

The owner runs this (make embed-reference): it downloads PyTorch and the
model's PyTorch weights.
"""

import json
import sys

from sentence_transformers import SentenceTransformer

MODEL = "intfloat/multilingual-e5-small"
REVISION = "614241f622f53c4eeff9890bdc4f31cfecc418b3"


def main(src: str, out: str) -> None:
    with open(src, encoding="utf-8") as f:
        texts = json.load(f)["texts"]
    model = SentenceTransformer(MODEL, revision=REVISION, device="cpu")
    vectors = model.encode(texts, normalize_embeddings=True, batch_size=8)
    ids = model.tokenizer(texts, truncation=True, max_length=model.max_seq_length)[
        "input_ids"
    ]
    result = {
        "model": MODEL,
        "revision": REVISION,
        "maxSeqLength": model.max_seq_length,
        "texts": [
            {"text": t, "ids": i, "vector": [round(float(x), 7) for x in v]}
            for t, i, v in zip(texts, ids, vectors, strict=True)
        ],
    }
    with open(out, "w", encoding="utf-8") as f:
        json.dump(result, f, ensure_ascii=False)
    print(f"{len(texts)} texts, max_seq_length {model.max_seq_length}: {out}")


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
