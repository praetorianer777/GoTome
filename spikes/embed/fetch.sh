#!/usr/bin/env bash
# Downloads what the embedding spike runs on into .cache/embed/: the model
# multilingual-e5-small at a fixed revision, with its fp32 and int8 ONNX
# weights and its tokenizer, and ONNX Runtime for x86-64 and arm64. Every
# large file is checked against the SHA-256 its source publishes; the small
# JSON files come from the same fixed revision.
#
# The agent does not download from outside; the owner runs this
# (make embed-assets). A file already there and correct is not fetched again.
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
dest=$root/.cache/embed
model=intfloat/multilingual-e5-small
revision=614241f622f53c4eeff9890bdc4f31cfecc418b3
ort=1.30.0

fetch() { # url file [sha256]
	local url=$1 file=$2 sum=${3:-}
	if [[ -f $file && ( -z $sum || $(sha256sum "$file" | cut -d' ' -f1) == "$sum" ) ]]; then
		echo "have $(basename "$file")"
		return
	fi
	echo "fetching $url"
	curl -fL --retry 3 -o "$file.part" "$url"
	if [[ -n $sum ]] && ! echo "$sum  $file.part" | sha256sum -c --quiet -; then
		echo "checksum of $url does not match; nothing kept" >&2
		rm -f "$file.part"
		exit 1
	fi
	mv "$file.part" "$file"
}

mkdir -p "$dest/model"
hf=https://huggingface.co/$model/resolve/$revision/onnx
fetch "$hf/model.onnx" "$dest/model/model.onnx" ca456c06b3a9505ddfd9131408916dd79290368331e7d76bb621f1cba6bc8665
fetch "$hf/model_qint8_avx512_vnni.onnx" "$dest/model/model_qint8_avx512_vnni.onnx" dd476dd0c2514e9b9be83aeb3853fac0763e0bdf4a71645407587d77c48a2d88
fetch "$hf/tokenizer.json" "$dest/model/tokenizer.json" 0b44a9d7b51c3c62626640cda0e2c2f70fdacdc25bbbd68038369d14ebdf4c39
for f in config.json special_tokens_map.json tokenizer_config.json; do
	fetch "$hf/$f" "$dest/model/$f"
done

gh=https://github.com/microsoft/onnxruntime/releases/download/v$ort
fetch "$gh/onnxruntime-linux-x64-$ort.tgz" "$dest/onnxruntime-linux-x64-$ort.tgz" a5ed5a3cac51fbb2e90da632ae43d19212faaa20e76484e62bcb7c23ddb3b3fd
fetch "$gh/onnxruntime-linux-aarch64-$ort.tgz" "$dest/onnxruntime-linux-aarch64-$ort.tgz" e16a27a8ed330bbc698df7330b0cf56e722f354e3bcc92118682c74ef3c3e3da
for arch in x64 aarch64; do
	[[ -d $dest/onnxruntime-linux-$arch-$ort ]] || tar -xzf "$dest/onnxruntime-linux-$arch-$ort.tgz" -C "$dest"
done

echo "done: $dest"
