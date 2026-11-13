### transfer: 221 test books, 5 genres

| Method | P | R | F1 | macro F1 | given | genres abstaining | best single threshold: F1 (P, R, given) |
|---|---|---|---|---|---|---|---|
| tfidf | 0.42 | 0.18 | 0.25 | 0.15 | 45% | 3 | 0.25 at 0.40 (0.34, 0.20, 61%) |
| tfidf1000 | 0.10 | 0.08 | 0.09 | 0.04 | 82% | 4 | 0.22 at 0.20 (0.15, 0.43, 99%) |
| mean | 0.71 | 0.60 | 0.65 | 0.47 | 78% | 0 | 0.72 at 0.20 (0.69, 0.75, 94%) |
| passages-topk | 0.75 | 0.38 | 0.50 | 0.35 | 52% | 2 | 0.57 at 0.90 (0.46, 0.75, 97%) |
| passages-share | 0.64 | 0.31 | 0.42 | 0.33 | 49% | 1 | 0.59 at 0.35 (0.51, 0.69, 94%) |
| passages-mean | 0.65 | 0.38 | 0.48 | 0.36 | 59% | 1 | 0.60 at 0.40 (0.55, 0.66, 91%) |
| meta | 0.60 | 0.01 | 0.02 | 0.03 | 2% | 3 | 0.36 at 0.10 (0.29, 0.46, 96%) |
| neighbours | 0.34 | 0.07 | 0.12 | 0.07 | 23% | 4 | 0.44 at 0.20 (0.30, 0.78, 99%) |
| ens-all | 0.73 | 0.65 | 0.68 | 0.53 | 82% | 0 | 0.71 at 0.60 (0.71, 0.71, 90%) |
| ens-text | 0.68 | 0.63 | 0.65 | 0.47 | 84% | 0 | 0.70 at 0.60 (0.69, 0.70, 89%) |
| ens-embed | 0.77 | 0.62 | 0.69 | 0.54 | 77% | 0 | 0.74 at 0.50 (0.68, 0.80, 96%) |
| ens-light | 0.61 | 0.40 | 0.48 | 0.35 | 64% | 2 | 0.59 at 0.55 (0.53, 0.66, 95%) |
| ens-nopassages | 0.71 | 0.62 | 0.66 | 0.49 | 82% | 1 | 0.71 at 0.60 (0.71, 0.70, 89%) |

Methods on de books (en, de by language; desc, nodesc by whether the file has a description):

| Method | P | R | F1 | given |
|---|---|---|---|---|
| tfidf | 0.42 | 0.18 | 0.25 | 45% |
| mean | 0.71 | 0.60 | 0.65 | 78% |
| meta | 0.60 | 0.01 | 0.02 | 2% |
| ens-all | 0.73 | 0.65 | 0.68 | 82% |
| ens-text | 0.68 | 0.63 | 0.65 | 84% |
| ens-embed | 0.77 | 0.62 | 0.69 | 77% |
| ens-light | 0.61 | 0.40 | 0.48 | 64% |
| ens-nopassages | 0.71 | 0.62 | 0.66 | 82% |

Methods on desc books (en, de by language; desc, nodesc by whether the file has a description):

| Method | P | R | F1 | given |
|---|---|---|---|---|
| tfidf | 0.62 | 0.22 | 0.33 | 39% |
| mean | 0.59 | 0.49 | 0.53 | 75% |
| meta | 0.67 | 0.01 | 0.03 | 2% |
| ens-all | 0.66 | 0.55 | 0.60 | 79% |
| ens-text | 0.59 | 0.53 | 0.56 | 81% |
| ens-embed | 0.67 | 0.50 | 0.57 | 73% |
| ens-light | 0.62 | 0.38 | 0.47 | 66% |
| ens-nopassages | 0.63 | 0.52 | 0.57 | 80% |

Methods on nodesc books (en, de by language; desc, nodesc by whether the file has a description):

| Method | P | R | F1 | given |
|---|---|---|---|---|
| tfidf | 0.22 | 0.12 | 0.15 | 54% |
| mean | 0.90 | 0.76 | 0.82 | 82% |
| meta | 0.50 | 0.01 | 0.02 | 2% |
| ens-all | 0.82 | 0.79 | 0.80 | 87% |
| ens-text | 0.80 | 0.77 | 0.78 | 88% |
| ens-embed | 0.89 | 0.79 | 0.84 | 83% |
| ens-light | 0.60 | 0.42 | 0.49 | 62% |
| ens-nopassages | 0.81 | 0.76 | 0.78 | 85% |

Per genre at the calibrated thresholds (P / R, support):

| Genre | n | tfidf | passages-topk | meta | neighbours | ens-all |
|---|---|---|---|---|---|---|
| crime | 103 | - | 0.88 / 0.50 | 0.50 / 0.01 | - | 0.81 / 0.83 |
| fantasy | 35 | - | - | - | - | 0.75 / 0.09 |
| historical | 21 | 0.20 / 0.10 | - | - | - | 0.43 / 0.14 |
| romance | 44 | 0.44 / 0.91 | 0.55 / 0.41 | - | 0.34 / 0.39 | 0.79 / 0.75 |
| science-fiction | 32 | - | 0.70 / 0.59 | 0.67 / 0.06 | - | 0.55 / 0.88 |
