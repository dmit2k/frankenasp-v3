# External ASP V3 — descriptor v1 wire contract (Stage02)

Status: **codec-only**, not a live helper or DSP admission. All integers are explicit
little-endian; no on-wire C/Go struct copies, TLV, envelopes, negotiation, or CFG2.

## Descriptor layout

Exactly one descriptor per session: 24-byte header, N consecutive 28-byte
physical buffer records, M consecutive 8-byte channel records, then three
variable-length raw source sections in fixed order. No trailers.

| Header offset | Field |
|---|---|
| 0 | ASCII ASP1 (4 bytes) |
| 4 | uint16 version = 1 |
| 6,7 | uint8 buffer count N (1..255), channel count M (1..255) |
| 8,12 | uint32 descriptor size, total input PCM block size |
| 16,18 | uint16 platform section length, hardware section length |
| 20 | uint32 factory section length |

Descriptor size = 24 + N*28 + M*8 + all three section lengths.
No reserved header bytes exist; all 24 bytes are occupied.

**Buffer record** (offsets relative to its first byte): 0 uint32 input offset,
4 uint32 physical byte length, 8 uint32 sample rate (Hz), 12 uint32 frame
count, 16 uint32 frame stride (bytes), 20 uint8 format, 21 uint8 valid bits,
22 uint8 LSB shift, 23 uint8 channels, 24 uint16 first channel record,
26 uint16 reserved (= 0). Formats 1 S16LE, 2 S24_3LE, 3 S32LE.
Valid bits include sign and range from 2 to storage width; LSB shift is
0..(storage width - valid bits). Signed24 in S32 uses shift 0 or 8.

**Channel record**: byte 0 uint8 physical buffer index; byte 1 uint8 role
(1 MIC / 2 REF); bytes 2..3 uint16 role index; bytes 4..7 uint32 byte
offset within a frame. Role index is a captured slot, not a mapping to
physical mic positions or factory gain. MIC indices and REF indices each
form independent 0..count-1 sequences. At least one MIC, REF optional.
The ordered records are grouped by physical buffer and cover its declared
channels once; first_channel sequences begin at zero. Channels must have
nonoverlapping sample-width intervals within their frame stride.

Buffers transfer byte-exact, in record order, with offsets starting at 0
and incrementing by each physical length; the sum must equal header input
bytes. Every channel must satisfy both `offset + sampleWidth <= stride`
and `(frames-1)*stride + offset + sampleWidth <= physicalLength`,
with 64-bit intermediates. Positive sample rate, frame count, stride
and physical length are required. Valid descriptor geometry is not vendor
DSP qualification. All physical PCM bytes, including stereo REF L/R, pass
unmodified; there is no host downmix, reordering, resampling, or calibration.

## Three raw sections

The header's three lengths delimit the **fixed-category** sections:
1 platform/identity, 2 capture hardware, 3 factory/IDME. A section with
length 0 is absent; it contains no count prefix and no synthetic defaults.

Each present source entry within its section is:

- uint8 source_name_length (1..63);
- uint16 little-endian raw_value_length (0..32768);
- source_name_length bytes of source identity, ASCII bytes `[A-Za-z0-9/_.-]`;
- exactly raw_value_length **original, uninterpreted bytes**, including
  embedded NUL, trailing whitespace and bytes >= 0x80.

The source identity is a literal name/path of the **actual upstream source**
(e.g. `/proc/idme/miccal.0`), not a Go-inferred platform identity. The
fixed header's category plus source name identifies the value. No tags,
type IDs, semantic IDME parsing, TLV registry or derived calibration.
Entries are ordered as provided, source names must be unique *within*
a section, and zero-length present values are distinguishable from absent
entries. The category is **not** duplicated per entry. Source identity
is wire metadata; it is not a substitute for storing the raw source bytes.

Bounded implementation policy: descriptor <= **65536 bytes** (including all
records and raw sections), <=64 total raw entries, <=32768 bytes per raw
value, and platform/hardware <=65535 bytes by header width. C reads the
24-byte header before allocating the exact advertised descriptor size;
it does not allocate 65536 bytes per PCM frame. The 64 KiB bound permits
a 12,000-byte `miccal.0` raw test vector, which the old 8192-byte
normalized-metadata bound could not represent. TECHO5 stock sources
`/proc/idme/<field>` and `/proc/device-tree/idme/<field>/value` and
`miccal.0`..`miccal.3` were inspected; this is a conservative policy
cap, **not** a device measurement or proof that all future factory
source lengths fit. Oversize is a wire rejection, never silent truncation.

The host must later read actual raw source bytes. In current TECHO5,
`layout.Idme()` trims NUL/whitespace and `parseIdme()` produces a
normalized string, so neither is an acceptable raw value provider.
Stage02 intentionally adds no capture, IDME reader or DSP adapter.

## Readiness and PCM

On successful DSP initialization the helper writes **one byte 0xA5**
(hex `a5`) to its isolated PCM stdout/wire FD, once, before the first
exchange. Host must read exactly one byte and require equality. No ASCII
READY, status codes or other handshake messages. Failure closes the channel
or terminates, optionally explaining on stderr; this Stage does not implement
the launch or readiness state machine. After readiness each exchange is
unframed: exactly header.total_input_bytes input via stdin, exactly
**640 bytes** output (320 mono signed 16-bit LE samples, 16 kHz/20 ms).
The descriptor magic never appears in per-frame PCM.

The independent Python spec-derived fixture generator is
`tests/make_vectors.py`. Immutable binary vectors, textual C decode
transcripts, stereo raw PCM markers, readiness and SHA256SUMS live in
`testdata/` in **both** repositories. Go writer tests byte-compare against
these independent binaries; compiled C reader tests the transcripts and
sample markers. Compiler sanitizers, malformed inputs and unaligned decode
are exercised offline. They do not demonstrate vendor DSP compatibility,
actual hardware factory bytes or session/runtime correctness.
