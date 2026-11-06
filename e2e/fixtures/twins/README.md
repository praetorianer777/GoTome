# Twin books

One small EPUB twice, byte for byte, in two folders: two books of one
library that the duplicate check pairs as the same file. The development
stack mounts the folder once per browser project as `/fixtures/twins-<project>`,
read-only, and as the `books` folder of `/fixtures/shelf-<project>`, a folder
GOtome may write to whose `.trash` it may not (`deploy/docker-compose.dev.yml`).
