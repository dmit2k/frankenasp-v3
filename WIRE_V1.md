# External ASP V3 — PCM-only descriptor v1 (Stage02-R)

This document describes the **incompatible pre-deployment replacement** of the
old 24/28/8 descriptor. No legacy decoder, version negotiation, TLV,
platform identification, factory metadata or calibration appears on this wire.
The only authority for this correction is `PCM_ONLY_ARCHITECTURE_DECISION.md`.

## Exact little-endian wire

Descriptor = 16-byte header + N × 28-byte physical-buffer records +
M × 8-byte captured-channel records. No padding or other sections.
N and M are each 1..255; descriptor size is **exactly**
`16+28*N+8*M`, maximum **9196 bytes**. The input PCM block size is
independent and is not constrained to 9196 bytes.

| Header byte offset | Field / representation |
|---|---|
| 0–3 | ASCII `ASP1` |
| 4–5 | uint16 little-endian version 1 |
| 6 | uint8 physical buffer count N |
| 7 | uint8 channel count M |
| 8–11 | uint32 little-endian exact descriptor length |
| 12–15 | uint32 little-endian total PCM input bytes |

The **first buffer record starts at byte 16**, with no 8-byte metadata
header. Physical buffer record offsets: 0/4/8/12/16 uint32 offset,
physical length, sample rate, frames, stride; offsets 20/21/22/23
uint8 PCM format, valid bits including sign, LSB shift, channel count;
24 uint16 first channel index; 26 uint16 reserved=0.

PCM formats: 1 = S16LE, 2 = packed S24_3LE, 3 = S32LE; all signed
two's-complement. Valid bits are 2..storage bits and LSB shift is
0..storage-valid. Signed24 stored in S32 is low-aligned shift 0 or
high-aligned shift 8. No transformations occur in the generic codec.

Channel record (8 bytes): uint8 physical-buffer index; uint8 role
1=MIC/2=REF; uint16 role index; uint32 sample byte offset in each frame.
Role indices are independent contiguous captured MIC and REF slot
sequences, not factory calibration assignments. At least one MIC;
REF absent is legal. Channels for each buffer occupy consecutive
records covering all declared channels exactly once.

Physical buffers have contiguous byte offsets from zero, lengths
adding to the uint32 total input size without overflow. Each channel
sample must fit within one frame stride and within the last sample of
the physical buffer: `channel_offset+width <= stride` and
`(frames-1)*stride+channel_offset+width <= physical_length`, using
widened integer arithmetic. Buffer records allow unused padding bytes
but channel sample intervals must not overlap. Native interleaved and
separate MIC/REF are permitted. Both hardware REF stereo channels L/R
must retain their original bytes and physical order; no downmix,
scaling, reorder, resample or synthetic REF.

**No metadata**: there is no Platform, Hardware, Factory, RawField,
source identity/name, value length or metadata section anywhere
in the descriptor, including its tail. No host IDME access is needed
for encoding. Stock-only factory readers remain unaffected; removal
of the Stage03 External raw readers belongs to Stage03-R.

One-time READY is one byte **0xA5**, unchanged. After readiness
unframed synchronous PCM exchange uses the descriptor's total input
length and returns exactly 640 bytes = 320 mono S16LE samples at
16 kHz / 20 ms. No per-frame counters, messages, magic or statuses.
No vendor-DSP admission or live process is implemented by this codec.

## Independent proof

`tests/make_vectors.py` is an independently written Python
`struct.pack` oracle, not the Go or C production writer. Both repos
carry the same `testdata/SHA256SUMS`, exact descriptor binaries,
C parsed transcripts, readiness byte and stereo sample fixtures.
The `max_255` fixture is exactly 9196 bytes. C rejection tests include
legacy 24-byte descriptors with unchanged ASP1/version 1, malformed
sizes, metadata trailing bytes, short reads, invalid PCM geometry
and buffer integer overflow. Unaligned C parser reads and ASan/UBSan
are exercised separately. Test-only `--strict` checks an isolated
descriptor stream has no trailing bytes; normal session reads are
sized and may be immediately followed by valid PCM bytes.
