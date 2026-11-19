The default genre ensemble (#528, #529): numbers only, no title, author or text.
`spikes/genres/run.sh ship` writes these files, the same bytes on every run;
`ensemble.json` carries `version`, the `corpus` revision and the books
`trained` on per genre. See `docs/decisions/genre-detection.md`.
