#!/bin/sh
set -eu

src_dir=$1
dst_dir=$2
report=${3:-}
quality_threshold=${TEXTURE_SSIM_THRESHOLD:-0.9}
tmp_dst=$(mktemp -d "${dst_dir}.tmp.XXXXXX")
tmp_sources=$(mktemp)
current_tmp_dir=
cleanup() {
	rm -rf "$tmp_dst" "$current_tmp_dir" "$tmp_sources"
}
on_signal() {
	cleanup
	exit 130
}
trap cleanup EXIT
trap on_signal HUP INT TERM

if [ -n "$report" ]; then
	mkdir -p "$(dirname "$report")"
	printf '%s\n' 'source,selected_format,source_bytes,output_bytes,palette,ssim,decision' > "$report"
fi

find "$src_dir" -maxdepth 1 -type f -print | sort | while IFS= read -r src; do
	case "$(basename "$src")" in
		*.jpg|*.jpeg|*.png|*.dds|*.tga|*.bmp|*.dib)
			printf '%s\n' "$src" >> "$tmp_sources"
			;;
	esac
done
total=$(wc -l < "$tmp_sources")
current=0
while IFS= read -r src; do
	name=$(basename "$src")

	current=$((current + 1))
	printf '[%d/%d] %s\n' "$current" "$total" "$name" >&2
	base=${name%.*}
	tmp_dir=$(mktemp -d)
	current_tmp_dir=$tmp_dir

	# Compare candidates with the decoded source, not the original compressed
	# bytes. This measures the visible effect of palette reduction only.
	best_file=
	best_size=
	best_colors=
	best_ssim=
	for colors in 256 128 64 32 16 8 4 2; do
		candidate="$tmp_dir/$base-$colors.png"
		magick "$src" -colors "$colors" -dither None -strip \
			-define png:compression-level=9 "$candidate"
		ssim=$(compare -colorspace sRGB -metric SSIM "$src" "$candidate" null: 2>&1 | \
			awk 'match($0, /[0-9]+(\.[0-9]+)?/) { print substr($0, RSTART, RLENGTH); exit }')
		[ -n "$ssim" ] || ssim=0
		size=$(wc -c < "$candidate")
		printf '  palette=%-3s ssim=%s size=%s\n' "$colors" "$ssim" "$size" >&2
		if awk -v score="$ssim" -v threshold="$quality_threshold" 'BEGIN { exit !(score >= threshold) }'; then
			best_file=$candidate
			best_size=$size
			best_colors=$colors
			best_ssim=$ssim
		else
			printf '  rejected: SSIM below threshold %s\n' "$quality_threshold" >&2
			break
		fi
	done

	# The source is always a candidate: preserve its format when no PNG is
	# both within the quality budget and smaller than the stripped source.
	stripped="$tmp_dir/$name"
	magick "$src" -strip "$stripped"
	source_size=$(wc -c < "$stripped")
	if [ -n "$best_size" ] && [ "$best_size" -lt "$source_size" ]; then
		cp "$best_file" "$tmp_dst/$base.png"
		selected_format=png
		output_size=$best_size
		decision=palette
	else
		cp "$stripped" "$tmp_dst/$name"
		selected_format=${name##*.}
		output_size=$source_size
		best_colors=source
		best_ssim=1
		decision=source
	fi
	printf '  => %s (%s bytes, palette=%s, ssim=%s)\n' \
		"$decision" "$output_size" "$best_colors" "$best_ssim" >&2

	if [ -n "$report" ]; then
		printf '%s,%s,%s,%s,%s,%s,%s\n' \
			"$name" "$selected_format" "$(wc -c < "$src")" "$output_size" \
			"$best_colors" "$best_ssim" "$decision" >> "$report"
	fi
	rm -rf "$tmp_dir"
current_tmp_dir=
done < "$tmp_sources"

rm -rf "$dst_dir"
mv "$tmp_dst" "$dst_dir"
