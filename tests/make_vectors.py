#!/usr/bin/env python3
"""Independent specification-derived wire vectors: Python struct packing, never Go/C codec."""
import hashlib
import pathlib
import struct

OUT = pathlib.Path(__file__).resolve().parent.parent / "testdata"
OUT.mkdir(exist_ok=True)

def buf(offset, length, rate, frames, stride, fmt, valid, shift, channels, first):
    return (offset, length, rate, frames, stride, fmt, valid, shift, channels, first)

def ch(buffer_index, role, role_index, offset):
    return (buffer_index, role, role_index, offset)

def entry(source, value):
    return (source, value)

def section(fields):
    payload = bytearray()
    for name, value in fields:
        name = name.encode("ascii")
        assert 0 < len(name) <= 63 and len(value) <= 32768
        payload += struct.pack("<BH", len(name), len(value)) + name + value
    return bytes(payload)

def vector(name, buffers, channels, platform=(), hardware=(), factory=(), pcm=None):
    p, h, f = section(platform), section(hardware), section(factory)
    total_input = sum(x[1] for x in buffers)
    body = b"".join(struct.pack("<IIIIIBBBBHH", *b, 0) for b in buffers)
    body += b"".join(struct.pack("<BBHI", *c) for c in channels)
    body += p + h + f
    fullsize = 24 + len(body)
    assert fullsize <= 65536
    header = struct.pack("<4sHBBIIHHI", b"ASP1", 1, len(buffers), len(channels),
                         fullsize, total_input, len(p), len(h), len(f))
    data = header + body
    assert len(data) == fullsize
    (OUT / (name + ".bin")).write_bytes(data)
    expected = [f"D {fullsize} {total_input} {len(buffers)} {len(channels)} {len(platform)+len(hardware)+len(factory)}"]
    for i, b in enumerate(buffers):
        expected.append("B " + " ".join(map(str, b + (i,))))
    for c in channels:
        expected.append("C " + " ".join(map(str, c)))
    for section_id, fields in ((1, platform), (2, hardware), (3, factory)):
        for source, value in fields:
            expected.append(f"R {section_id} {source} {len(value)}" + (" " + value.hex() if value else ""))
    (OUT / (name + ".expect")).write_text("\n".join(expected) + "\n")
    if pcm is not None:
        assert len(pcm) == total_input
        (OUT / (name + ".pcm")).write_bytes(pcm)

# Real source identifiers from TECHO5 layout/readIDME and mic/device_cronos.
# Payload values are deterministic offline test bytes; no real device IDME is claimed.
stereo_pcm = (
    bytes.fromhex("010203 111213 212223 313233 a1b2c3 d4e5f6") +
    bytes.fromhex("040506 141516 242526 343536 a7b8c9 daebfc")
)
vector("stereo_s24", [buf(0,36,16000,2,18,2,24,0,6,0)],
       [ch(0,1,i,i*3) for i in range(4)] + [ch(0,2,0,12), ch(0,2,1,15)],
       [entry("/proc/idme/device_type_id", b"AB\x00\xff")],
       [entry("/proc/device-tree/idme/hw_rev/value", b"v1\x00\x80")],
       [entry("/proc/idme/miccal.0", b"\x00\x81\xff\x7f"),
        entry("/proc/idme/miccal.1", b"0\x00A\xfe")],
       pcm=stereo_pcm)
vector("separate_three", [buf(0,8,16000,2,4,1,16,0,2,0),
                         buf(8,4,16000,2,2,1,16,0,1,2),
                         buf(12,4,16000,2,2,1,16,0,1,3)],
       [ch(0,1,0,0),ch(0,1,1,2),ch(1,2,0,0),ch(2,2,1,0)],
       [entry("/proc/idme/productid2", b"")], pcm=bytes.fromhex(
       "0102 0304 0506 0708 1122 3344 5566 7788"))
vector("s32_low24", [buf(0,16,48000,2,8,3,24,0,2,0)],
       [ch(0,1,0,0),ch(0,2,0,4)])
vector("s32_high24", [buf(0,16,48000,2,8,3,24,8,2,0)],
       [ch(0,1,0,0),ch(0,2,0,4)])
vector("ref_absent", [buf(0,8,16000,2,4,1,16,0,2,0)],
       [ch(0,1,0,0),ch(0,1,1,2)])
vector("packed_sparse", [buf(0,24,16000,2,12,2,20,4,2,0)],
       [ch(0,2,0,7), ch(0,1,0,0)])
# Larger than the old 8192-byte normalized layout, with raw factory bytes kept intact.
big_raw = bytes((i * 37 + 0x80) & 255 for i in range(12000))
vector("large_factory", [buf(0,4,16000,2,2,1,16,0,1,0)],
       [ch(0,1,0,0)], factory=[entry("/proc/idme/miccal.0",big_raw)])
(OUT / "readiness.bin").write_bytes(b"\xa5")
(OUT / "output_contract.txt").write_text("mono S16LE 16000 Hz 320 samples 20 ms 640 bytes\n")
files = sorted(x for x in OUT.iterdir() if x.is_file() and x.name != "SHA256SUMS")
manifest = "".join(f"{hashlib.sha256(x.read_bytes()).hexdigest()}  {x.name}\n" for x in files)
(OUT / "SHA256SUMS").write_text(manifest)
print(manifest)
