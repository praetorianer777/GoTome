### adapted: 221 test books, 5 genres

| Method | P | R | F1 | macro F1 | given | genres abstaining | best single threshold: F1 (P, R, given) |
|---|---|---|---|---|---|---|---|
| tfidf | 0.75 | 0.68 | 0.71 | 0.67 | 89% | 0 | 0.74 at 0.35 (0.75, 0.73, 92%) |
| tfidf1000 | 0.69 | 0.58 | 0.63 | 0.54 | 67% | 0 | 0.48 at 0.30 (0.35, 0.81, 99%) |
| mean | 0.78 | 0.70 | 0.74 | 0.67 | 86% | 0 | 0.78 at 0.35 (0.77, 0.79, 95%) |
| passages-topk | 0.84 | 0.51 | 0.63 | 0.41 | 64% | 1 | 0.72 at 0.95 (0.70, 0.75, 91%) |
| passages-share | 0.78 | 0.58 | 0.66 | 0.55 | 76% | 0 | 0.76 at 0.45 (0.73, 0.79, 95%) |
| passages-mean | 0.78 | 0.65 | 0.71 | 0.60 | 81% | 0 | 0.76 at 0.45 (0.71, 0.83, 97%) |
| meta | 0.68 | 0.11 | 0.18 | 0.21 | 16% | 1 | 0.50 at 0.25 (0.47, 0.54, 89%) |
| neighbours | 0.79 | 0.51 | 0.62 | 0.55 | 67% | 0 | 0.71 at 0.40 (0.67, 0.75, 99%) |
| ens-all | 0.74 | 0.77 | 0.76 | 0.73 | 93% | 0 | 0.79 at 0.70 (0.79, 0.79, 93%) |
| ens-text | 0.75 | 0.74 | 0.75 | 0.71 | 92% | 0 | 0.78 at 0.75 (0.80, 0.77, 93%) |
| ens-embed | 0.78 | 0.74 | 0.76 | 0.71 | 90% | 0 | 0.79 at 0.75 (0.83, 0.77, 91%) |
| ens-light | 0.79 | 0.63 | 0.70 | 0.57 | 77% | 1 | 0.78 at 0.70 (0.76, 0.79, 93%) |
| ens-nopassages | 0.75 | 0.77 | 0.76 | 0.73 | 93% | 0 | 0.78 at 0.60 (0.75, 0.81, 96%) |

Methods on de books (en, de by language; desc, nodesc by whether the file has a description):

| Method | P | R | F1 | given |
|---|---|---|---|---|
| tfidf | 0.75 | 0.68 | 0.71 | 89% |
| mean | 0.78 | 0.70 | 0.74 | 86% |
| meta | 0.68 | 0.11 | 0.18 | 16% |
| ens-all | 0.74 | 0.77 | 0.76 | 93% |
| ens-text | 0.75 | 0.74 | 0.75 | 92% |
| ens-embed | 0.78 | 0.74 | 0.76 | 90% |
| ens-light | 0.79 | 0.63 | 0.70 | 77% |
| ens-nopassages | 0.75 | 0.77 | 0.76 | 93% |

Methods on desc books (en, de by language; desc, nodesc by whether the file has a description):

| Method | P | R | F1 | given |
|---|---|---|---|---|
| tfidf | 0.62 | 0.56 | 0.59 | 88% |
| mean | 0.72 | 0.59 | 0.65 | 83% |
| meta | 0.82 | 0.16 | 0.27 | 21% |
| ens-all | 0.66 | 0.69 | 0.67 | 92% |
| ens-text | 0.65 | 0.64 | 0.65 | 90% |
| ens-embed | 0.70 | 0.65 | 0.67 | 89% |
| ens-light | 0.73 | 0.56 | 0.63 | 75% |
| ens-nopassages | 0.66 | 0.68 | 0.67 | 92% |

Methods on nodesc books (en, de by language; desc, nodesc by whether the file has a description):

| Method | P | R | F1 | given |
|---|---|---|---|---|
| tfidf | 0.93 | 0.85 | 0.89 | 89% |
| mean | 0.87 | 0.85 | 0.86 | 89% |
| meta | 0.22 | 0.02 | 0.04 | 10% |
| ens-all | 0.88 | 0.89 | 0.89 | 95% |
| ens-text | 0.89 | 0.89 | 0.89 | 95% |
| ens-embed | 0.88 | 0.87 | 0.88 | 91% |
| ens-light | 0.88 | 0.74 | 0.80 | 81% |
| ens-nopassages | 0.89 | 0.89 | 0.89 | 94% |

Per genre at the calibrated thresholds (P / R, support):

| Genre | n | tfidf | passages-topk | meta | neighbours | ens-all |
|---|---|---|---|---|---|---|
| crime | 103 | 0.80 / 0.79 | 0.85 / 0.84 | - | 0.81 / 0.60 | 0.83 / 0.81 |
| fantasy | 35 | 0.33 / 0.31 | - | 0.50 / 0.03 | 0.85 / 0.31 | 0.46 / 0.51 |
| historical | 21 | 1.00 / 0.48 | 1.00 / 0.05 | 0.40 / 0.10 | 0.67 / 0.19 | 0.82 / 0.67 |
| romance | 44 | 0.80 / 0.80 | 0.86 / 0.43 | 0.71 / 0.23 | 0.73 / 0.61 | 0.80 / 0.89 |
| science-fiction | 32 | 0.88 / 0.72 | 0.72 / 0.41 | 0.86 / 0.38 | 0.89 / 0.50 | 0.71 / 0.84 |
