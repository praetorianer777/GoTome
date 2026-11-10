#!/bin/sh
# Packs web/demo/files after checking every book against the lock file, and
# creates or updates the release $TAG with the tar as its asset.
set -eu
cd "$ROOT/web/demo"
tar_path="$ROOT/.cache/sample-books.tar"
mkdir -p "$ROOT/.cache"

ids=$(sed -n 's/^ "\(b-[^"]*\)": {$/\1/p' sample-books.lock.json)
[ -n "$ids" ] || { echo "The lock file pins no books."; exit 1; }
for id in $ids; do
  want=$(awk -v id="\"$id\"" '$1 == id":" {f=1} f && /"sha256"/ {gsub(/[",]/, "", $2); print $2; exit}' sample-books.lock.json)
  [ -f "files/$id.epub" ] || { echo "files/$id.epub is missing: run make sample-books first."; exit 1; }
  got=$(sha256sum "files/$id.epub" | cut -d' ' -f1)
  [ "$got" = "$want" ] || { echo "files/$id.epub is $got, the lock file pins $want."; exit 1; }
done

(cd files && tar -cf "$tar_path" $(for id in $ids; do echo "$id.epub"; done))

notes="The EPUBs the demo (docs/demo.md) is built from, as a fallback for CI when
Standard Ebooks or Project Gutenberg refuse its runners. All are public domain:
CC0 1.0 from Standard Ebooks, public domain from Project Gutenberg. CI checks
every file against web/demo/sample-books.lock.json before it uses one."

if gh release view "$TAG" >/dev/null 2>&1; then
  gh release upload "$TAG" "$tar_path" --clobber
  gh release edit "$TAG" --prerelease --notes "$notes"
else
  gh release create "$TAG" "$tar_path" --prerelease --title "Demo sample books" --notes "$notes"
fi
echo "Published $TAG with $(echo "$ids" | wc -l) books."
