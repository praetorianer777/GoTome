### library5: 221 test books, 5 genres

| Method | P | R | F1 | macro F1 | given | genres abstaining | best single threshold: F1 (P, R, given) |
|---|---|---|---|---|---|---|---|
| tfidf | 0.74 | 0.71 | 0.73 | 0.71 | 91% | 0 | 0.73 at 0.30 (0.71, 0.76, 95%) |
| tfidf1000 | 0.72 | 0.69 | 0.70 | 0.65 | 83% | 0 | 0.50 at 0.35 (0.38, 0.76, 99%) |
| mean | 0.69 | 0.79 | 0.74 | 0.70 | 96% | 0 | 0.79 at 0.50 (0.82, 0.76, 86%) |
| passages-topk | 0.65 | 0.61 | 0.63 | 0.58 | 81% | 0 | 0.67 at 0.95 (0.55, 0.85, 99%) |
| passages-share | 0.73 | 0.57 | 0.64 | 0.62 | 73% | 0 | 0.72 at 0.40 (0.72, 0.72, 87%) |
| passages-mean | 0.70 | 0.58 | 0.64 | 0.62 | 77% | 0 | 0.73 at 0.45 (0.79, 0.67, 81%) |
| meta | 0.68 | 0.35 | 0.46 | 0.40 | 47% | 1 | 0.59 at 0.15 (0.53, 0.66, 95%) |
| neighbours | 0.79 | 0.53 | 0.63 | 0.61 | 68% | 0 | 0.71 at 0.35 (0.67, 0.75, 99%) |
| ens-all | 0.76 | 0.67 | 0.71 | 0.65 | 84% | 0 | 0.79 at 0.65 (0.78, 0.81, 94%) |
| ens-text | 0.76 | 0.69 | 0.72 | 0.65 | 86% | 0 | 0.78 at 0.75 (0.80, 0.76, 91%) |
| ens-embed | 0.80 | 0.67 | 0.73 | 0.63 | 81% | 0 | 0.80 at 0.75 (0.82, 0.77, 89%) |
| ens-light | 0.70 | 0.58 | 0.64 | 0.60 | 75% | 0 | 0.72 at 0.85 (0.80, 0.66, 80%) |
| ens-nopassages | 0.75 | 0.71 | 0.73 | 0.67 | 87% | 0 | 0.78 at 0.65 (0.76, 0.80, 95%) |

Methods on de books (en, de by language; desc, nodesc by whether the file has a description):

| Method | P | R | F1 | given |
|---|---|---|---|---|
| tfidf | 0.74 | 0.71 | 0.73 | 91% |
| mean | 0.69 | 0.79 | 0.74 | 96% |
| meta | 0.68 | 0.35 | 0.46 | 47% |
| ens-all | 0.76 | 0.67 | 0.71 | 84% |
| ens-text | 0.76 | 0.69 | 0.72 | 86% |
| ens-embed | 0.80 | 0.67 | 0.73 | 81% |
| ens-light | 0.70 | 0.58 | 0.64 | 75% |
| ens-nopassages | 0.75 | 0.71 | 0.73 | 87% |

Methods on desc books (en, de by language; desc, nodesc by whether the file has a description):

| Method | P | R | F1 | given |
|---|---|---|---|---|
| tfidf | 0.62 | 0.61 | 0.61 | 91% |
| mean | 0.63 | 0.70 | 0.66 | 95% |
| meta | 0.73 | 0.34 | 0.47 | 44% |
| ens-all | 0.67 | 0.55 | 0.60 | 78% |
| ens-text | 0.67 | 0.57 | 0.62 | 82% |
| ens-embed | 0.73 | 0.54 | 0.62 | 75% |
| ens-light | 0.60 | 0.44 | 0.51 | 66% |
| ens-nopassages | 0.66 | 0.60 | 0.63 | 81% |

Methods on nodesc books (en, de by language; desc, nodesc by whether the file has a description):

| Method | P | R | F1 | given |
|---|---|---|---|---|
| tfidf | 0.92 | 0.87 | 0.90 | 91% |
| mean | 0.78 | 0.92 | 0.84 | 98% |
| meta | 0.62 | 0.36 | 0.45 | 52% |
| ens-all | 0.86 | 0.85 | 0.86 | 92% |
| ens-text | 0.89 | 0.86 | 0.88 | 90% |
| ens-embed | 0.88 | 0.86 | 0.87 | 90% |
| ens-light | 0.81 | 0.80 | 0.80 | 87% |
| ens-nopassages | 0.86 | 0.87 | 0.86 | 95% |

Per genre at the calibrated thresholds (P / R, support):

| Genre | n | tfidf | passages-topk | meta | neighbours | ens-all |
|---|---|---|---|---|---|---|
| crime | 103 | 0.82 / 0.77 | 0.86 / 0.63 | 0.68 / 0.29 | 0.84 / 0.51 | 0.81 / 0.83 |
| fantasy | 35 | 0.37 / 0.43 | 0.43 / 0.66 | - | 0.67 / 0.51 | 0.50 / 0.34 |
| historical | 21 | 0.75 / 0.71 | 0.61 / 0.52 | 0.62 / 0.48 | 0.88 / 0.33 | 0.83 / 0.71 |
| romance | 44 | 0.78 / 0.82 | 0.69 / 0.80 | 0.69 / 0.82 | 0.73 / 0.61 | 0.81 / 0.80 |
| science-fiction | 32 | 0.92 / 0.72 | 0.42 / 0.31 | 0.67 / 0.19 | 0.86 / 0.59 | 0.58 / 0.34 |
