# Embedding books on a GPU

GOtome works out what books are about with multilingual-e5-small, which it
runs itself on the CPU. A machine with a GPU can run a larger model through
[Ollama](https://ollama.com) instead: bge-m3 (BAAI), multilingual, 1,024
dimensions, five times e5-small's size.

## Setting it up

1. Run Ollama where the app's container can reach it, listening on more
   than the loopback address (`OLLAMA_HOST=0.0.0.0`), and pull the model:
   `ollama pull bge-m3`.
2. If Ollama runs on the Docker host, give the app the host's name:

   ```yaml
   services:
     app:
       extra_hosts:
         - "host.docker.internal:host-gateway"
   ```

3. In GOtome's settings, set **Embedding server** to
   `http://host.docker.internal:11434` and **Language model** to `bge-m3`.

Every book is then embedded again, four at a time. The vectors of the model
before stay: choosing it again is at once, and `gotome similar-check`
compares the two on the same library.

Each time it opens the model, GOtome checks the server's vectors: their
length, and that two sentences of one meaning in two languages lie nearer
each other than a third of another. A server that fails it, or does not
answer, is not used, and the log says why; the next pass tries again.

## Measured

On the owner's machine (AMD Radeon RX 9070, Ollama 0.30.7), with passages
of a library of German novels (#173):

| | Passages a second |
|---|---:|
| multilingual-e5-small on the CPU, in GOtome | about 6 |
| bge-m3 on the GPU, 16 at a time | 13 |
| bge-m3 on the GPU, four books of 16 at once | 17 |
| bge-m3 on the GPU, whole chunks of 8,000 characters | 3.5 |

bge-m3 reads 8,192 tokens, but GOtome asks it for the first 512 of each
passage, as e5-small reads them: whole chunks would take four times as
long. A library of 50,000 books takes about 14 hours on the GPU, against
about 35 on the CPU, and leaves the CPU free.
