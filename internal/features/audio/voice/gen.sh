#!/usr/bin/env bash
# Regenerates rex.pcm: the spoken "rex" boot voice, same recipe as the lifeOS
# boot voice (lifeOS plan/design.md section 7a, tools/make-voice.sh).
# Original synthesis only: macOS `say` (Ralph) robotized to a flat ~94 Hz by
# ffmpeg; no samples, no echo or other effects.
# The afftfilt zero-phase trick replaces the speaker's pitch with a flat buzz at
# 48000 / (WIN * 0.25) Hz: 2048 -> ~94 Hz. Rendered at 48 kHz so the pitch
# matches lifeOS, then resampled (pitch kept) to the player's rate.
# Output: raw signed 16-bit LE, mono, 44100 Hz (must match synth.go).
set -euo pipefail
cd "$(dirname "$0")"
VOICE="${VOICE:-Ralph}"
WORD="${WORD:-rex}"
WIN="${WIN:-2048}"
RATE="${RATE:-140}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

say -v "$VOICE" -r "$RATE" -o "$tmp/raw.aiff" "$WORD"
ffmpeg -loglevel error -y -i "$tmp/raw.aiff" -af \
  "aresample=48000,afftfilt=real='hypot(re,im)*sin(0)':imag='hypot(re,im)*cos(0)':win_size=$WIN:overlap=0.75,highpass=f=60,lowpass=f=6000,loudnorm=I=-16:TP=-3" \
  -ac 1 -ar 48000 "$tmp/pass1.wav"

# one-pass loudnorm undershoots on sub-second words; when it lands under -18 LUFS, add the missing gain
# and hold true peak under -3 dBFS with a limiter. The limiter eats some of the gain, so retry with more.
loudness() { ffmpeg -nostats -i "$1" -af ebur128=peak=true -f null - 2>&1 | awk '/^ +I:/ {v=$2} END {print v}'; }
lufs=$(loudness "$tmp/pass1.wav")
cp "$tmp/pass1.wav" "$tmp/voice.wav"
target=-16.5
for _ in 1 2 3 4 5 6; do
  awk -v l="$(loudness "$tmp/voice.wav")" 'BEGIN { exit !(l < -18) }' || break
  gain=$(awk -v l="$lufs" -v t="$target" 'BEGIN { printf "%.2f", t - l }')
  ffmpeg -loglevel error -y -i "$tmp/pass1.wav" -af "volume=${gain}dB,alimiter=limit=0.63:level=false:attack=5:release=150" \
    -ac 1 -ar 48000 "$tmp/voice.wav"
  target=$(awk -v t="$target" 'BEGIN { print t + 1.5 }')
done

ffmpeg -loglevel error -y -i "$tmp/voice.wav" -af aresample=44100 \
  -f s16le -acodec pcm_s16le -ar 44100 -ac 1 rex.pcm
ls -l rex.pcm
