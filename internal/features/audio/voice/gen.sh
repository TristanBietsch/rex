#!/usr/bin/env bash
# Regenerates rex.pcm: a VOX / HEV-suit style "rex" announcement synthesized
# from macOS `say` + ffmpeg. Original audio only; no third-party samples.
# Output: raw signed 16-bit LE, mono, 44100 Hz (must match synth.go).
set -euo pipefail
cd "$(dirname "$0")"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# Slow, flat delivery: Fred is naturally robotic; [[rate]] slows it, [[pbas]] drops pitch.
say -v Fred -o "$tmp/rex.aiff" "[[rate 110]][[pbas 38]] rex."

# Voice chain: trim silence, pitch down a touch, telephone band-limit,
# bit-crush, short metallic comb echo + light flange, compression, limit.
ffmpeg -y -loglevel error -i "$tmp/rex.aiff" -ar 44100 -ac 1 -af "\
silenceremove=start_periods=1:start_threshold=-45dB:stop_periods=1:stop_duration=0.15:stop_threshold=-45dB,\
asetrate=44100*0.93,aresample=44100,\
highpass=f=320,lowpass=f=3400,\
acrusher=bits=8:mode=lin:aa=0:samples=2,\
aecho=0.8:0.6:14|31:0.35|0.2,\
flanger=delay=2:depth=1:speed=0.3:width=60:regen=25,\
acompressor=threshold=-24dB:ratio=6:attack=5:release=60:makeup=4,\
highpass=f=300,lowpass=f=3600,\
alimiter=limit=0.89,\
apad=pad_dur=0.1" "$tmp/voice.wav"

# PA lead-in blip: two short sine beeps (own synth), bit-crushed.
ffmpeg -y -loglevel error \
  -f lavfi -i "sine=f=1100:d=0.07:r=44100" \
  -f lavfi -i "anullsrc=r=44100:cl=mono:d=0.04" \
  -f lavfi -i "sine=f=1480:d=0.09:r=44100" \
  -f lavfi -i "anullsrc=r=44100:cl=mono:d=0.12" \
  -filter_complex "[0][1][2][3]concat=n=4:v=0:a=1,volume=0.22,acrusher=bits=8:mode=lin:aa=0" \
  "$tmp/blip.wav"

ffmpeg -y -loglevel error -i "$tmp/blip.wav" -i "$tmp/voice.wav" \
  -filter_complex "[0][1]concat=n=2:v=0:a=1,aresample=44100,volume=7dB,alimiter=limit=0.9" \
  -f s16le -acodec pcm_s16le -ar 44100 -ac 1 rex.pcm
ls -l rex.pcm
