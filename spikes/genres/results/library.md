### library: 311 test books, 8 genres

| Method | P | R | F1 | macro F1 | given | genres abstaining | best single threshold: F1 (P, R, given) |
|---|---|---|---|---|---|---|---|
| tfidf | 0.54 | 0.25 | 0.34 | 0.35 | 49% | 1 | 0.64 at 0.30 (0.63, 0.66, 90%) |
| tfidf1000 | 0.58 | 0.17 | 0.27 | 0.28 | 36% | 1 | 0.36 at 0.45 (0.33, 0.41, 93%) |
| mean | 0.55 | 0.22 | 0.31 | 0.27 | 43% | 1 | 0.60 at 0.20 (0.54, 0.66, 92%) |
| passages-topk | 0.58 | 0.14 | 0.22 | 0.23 | 28% | 2 | 0.55 at 0.95 (0.47, 0.65, 94%) |
| passages-share | 0.57 | 0.11 | 0.19 | 0.21 | 24% | 3 | 0.57 at 0.35 (0.50, 0.67, 93%) |
| passages-mean | 0.60 | 0.16 | 0.25 | 0.24 | 32% | 3 | 0.56 at 0.35 (0.49, 0.66, 93%) |
| meta | 0.68 | 0.11 | 0.18 | 0.20 | 19% | 2 | 0.44 at 0.05 (0.36, 0.58, 97%) |
| neighbours | 0.67 | 0.06 | 0.12 | 0.11 | 13% | 2 | 0.53 at 0.25 (0.44, 0.67, 99%) |
| ens-all | 0.62 | 0.31 | 0.41 | 0.39 | 59% | 1 | 0.64 at 0.60 (0.55, 0.76, 98%) |
| ens-text | 0.62 | 0.30 | 0.40 | 0.37 | 56% | 1 | 0.64 at 0.65 (0.58, 0.72, 96%) |
| ens-embed | 0.60 | 0.15 | 0.24 | 0.25 | 31% | 1 | 0.58 at 0.55 (0.49, 0.70, 97%) |
| ens-light | 0.63 | 0.15 | 0.25 | 0.27 | 29% | 2 | 0.58 at 0.55 (0.47, 0.74, 98%) |
| ens-nopassages | 0.67 | 0.32 | 0.43 | 0.40 | 57% | 1 | 0.64 at 0.60 (0.54, 0.78, 99%) |

Methods on de books (en, de by language; desc, nodesc by whether the file has a description):

| Method | P | R | F1 | given |
|---|---|---|---|---|
| tfidf | 0.54 | 0.25 | 0.34 | 49% |
| mean | 0.55 | 0.22 | 0.31 | 43% |
| meta | 0.68 | 0.11 | 0.18 | 19% |
| ens-all | 0.62 | 0.31 | 0.41 | 59% |
| ens-text | 0.62 | 0.30 | 0.40 | 56% |
| ens-embed | 0.60 | 0.15 | 0.24 | 31% |
| ens-light | 0.63 | 0.15 | 0.25 | 29% |
| ens-nopassages | 0.67 | 0.32 | 0.43 | 57% |

Methods on desc books (en, de by language; desc, nodesc by whether the file has a description):

| Method | P | R | F1 | given |
|---|---|---|---|---|
| tfidf | 0.59 | 0.27 | 0.37 | 51% |
| mean | 0.52 | 0.21 | 0.30 | 46% |
| meta | 0.67 | 0.11 | 0.19 | 19% |
| ens-all | 0.62 | 0.29 | 0.40 | 57% |
| ens-text | 0.63 | 0.30 | 0.41 | 55% |
| ens-embed | 0.53 | 0.14 | 0.22 | 34% |
| ens-light | 0.57 | 0.15 | 0.24 | 33% |
| ens-nopassages | 0.61 | 0.30 | 0.40 | 58% |

Methods on nodesc books (en, de by language; desc, nodesc by whether the file has a description):

| Method | P | R | F1 | given |
|---|---|---|---|---|
| tfidf | 0.46 | 0.21 | 0.29 | 44% |
| mean | 0.62 | 0.23 | 0.34 | 37% |
| meta | 0.70 | 0.10 | 0.17 | 17% |
| ens-all | 0.63 | 0.33 | 0.43 | 63% |
| ens-text | 0.60 | 0.29 | 0.39 | 58% |
| ens-embed | 0.76 | 0.17 | 0.28 | 26% |
| ens-light | 0.77 | 0.16 | 0.26 | 23% |
| ens-nopassages | 0.80 | 0.35 | 0.49 | 54% |

Per genre at the calibrated thresholds (P / R, support):

| Genre | n | tfidf | passages-topk | meta | neighbours | ens-all |
|---|---|---|---|---|---|---|
| contemporary | 40 | - | - | - | 1.00 / 0.03 | - |
| crime | 103 | 0.69 / 0.24 | 0.67 / 0.12 | 0.80 / 0.08 | 0.50 / 0.02 | 0.80 / 0.38 |
| fantasy | 35 | 0.39 / 0.34 | 0.57 / 0.49 | - | - | 0.39 / 0.49 |
| historical | 21 | 0.72 / 0.62 | 0.68 / 0.62 | 0.67 / 0.48 | - | 0.79 / 0.52 |
| horror | 70 | 0.63 / 0.24 | 0.80 / 0.06 | 0.50 / 0.04 | 0.50 / 0.01 | 0.75 / 0.34 |
| romance | 44 | 0.73 / 0.43 | 0.57 / 0.09 | 0.70 / 0.32 | 0.33 / 0.02 | 0.84 / 0.48 |
| science-fiction | 32 | 0.44 / 0.38 | 0.33 / 0.19 | 0.60 / 0.19 | 0.73 / 0.59 | 0.59 / 0.31 |
| thriller | 60 | 0.14 / 0.05 | - | 1.00 / 0.03 | 1.00 / 0.03 | 0.11 / 0.03 |
