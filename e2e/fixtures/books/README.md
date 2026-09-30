# Fixture books

A small folder of books for the browser tests. The development stack mounts it
read-only into the app container, once per browser project, as
`/fixtures/books-<project>` (`deploy/docker-compose.dev.yml`), so a test can add
it as an external library without sharing the folder with another project.

The files are real but minimal: two EPUBs and a PDF, each a page long, with the
opening sentence of a novel that is in the public domain.
