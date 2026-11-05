#!/usr/bin/env python3
"""Independent PCM-only v1 exact-byte oracle (Python struct, no Go/C production encoder)."""
from pathlib import Path
import hashlib
import struct

OUT=Path(__file__).resolve().parent.parent/"testdata"
OUT.mkdir(exist_ok=True)

def buf(o,length,rate,frames,stride,fmt,valid,shift,count,first):
    return (o,length,rate,frames,stride,fmt,valid,shift,count,first)

def channel(physical,role,index,offset):
    return (physical,role,index,offset)

def vector(name,buffers,channels,pcm=None):
    assert 1<=len(buffers)<=255 and 1<=len(channels)<=255
    input_size=sum(b[1] for b in buffers)
    body=b"".join(struct.pack("<IIIIIBBBBHH",*b,0) for b in buffers)
    body+=b"".join(struct.pack("<BBHI",*c) for c in channels)
    size=16+len(body)
    assert size==16+28*len(buffers)+8*len(channels) and size<=9196
    hdr=struct.pack("<4sHBBII",b"ASP1",1,len(buffers),len(channels),size,input_size)
    wire=hdr+body
    assert len(wire)==size and len(hdr)==16
    (OUT/(name+".bin")).write_bytes(wire)
    expect=[f"D {size} {input_size} {len(buffers)} {len(channels)}"]
    expect+=["B "+" ".join(map(str,b+(i,))) for i,b in enumerate(buffers)]
    expect+=["C "+" ".join(map(str,c)) for c in channels]
    (OUT/(name+".expect")).write_text("\n".join(expect)+"\n")
    if pcm is not None:
        assert len(pcm)==input_size
        (OUT/(name+".pcm")).write_bytes(pcm)

stereo_pcm=(bytes.fromhex("010203 111213 212223 313233 a1b2c3 d4e5f6")
           +bytes.fromhex("040506 141516 242526 343536 a7b8c9 daebfc"))
vector("stereo_s24",
       [buf(0,36,16000,2,18,2,24,0,6,0)],
       [channel(0,1,i,i*3) for i in range(4)]+[channel(0,2,0,12),channel(0,2,1,15)],
       stereo_pcm)
vector("separate_three",
       [buf(0,8,16000,2,4,1,16,0,2,0),
        buf(8,4,16000,2,2,1,16,0,1,2),
        buf(12,4,16000,2,2,1,16,0,1,3)],
       [channel(0,1,0,0),channel(0,1,1,2),
        channel(1,2,0,0),channel(2,2,1,0)],
       bytes.fromhex("0102 0304 0506 0708 1122 3344 5566 7788"))
vector("s32_low24",
       [buf(0,16,48000,2,8,3,24,0,2,0)],
       [channel(0,1,0,0),channel(0,2,0,4)])
vector("s32_high24",
       [buf(0,16,48000,2,8,3,24,8,2,0)],
       [channel(0,1,0,0),channel(0,2,0,4)])
vector("ref_absent",
       [buf(0,8,16000,2,4,1,16,0,2,0)],
       [channel(0,1,0,0),channel(0,1,1,2)])
vector("packed_sparse",
       [buf(0,24,16000,2,12,2,20,4,2,0)],
       [channel(0,2,0,7),channel(0,1,0,0)])
# Exact representational maximum: 255 physical buffers, 255 captured MIC roles.
vector("max_255",
       [buf(2*i,2,16000,1,2,1,16,0,1,i) for i in range(255)],
       [channel(i,1,i,0) for i in range(255)])
(OUT/"readiness.bin").write_bytes(b"\xa5")
(OUT/"output_contract.txt").write_text("mono S16LE 16000 Hz 320 samples 20 ms 640 bytes\n")
# Only PCM-only fixtures survive; reject old metadata-bearing fixtures, do not retain them.
keep={"SHA256SUMS","readiness.bin","output_contract.txt"}
for name in ("stereo_s24","separate_three","s32_low24","s32_high24","ref_absent","packed_sparse","max_255"):
    keep|={name+".bin",name+".expect"}
keep|={"stereo_s24.pcm","separate_three.pcm"}
for p in OUT.iterdir():
    if p.is_file() and p.name not in keep:
        p.unlink()
files=sorted(p for p in OUT.iterdir() if p.is_file() and p.name!="SHA256SUMS")
manifest="".join(f"{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n" for p in files)
(OUT/"SHA256SUMS").write_text(manifest)
print("PCM-only fixtures",len(files),"manifest sha256",hashlib.sha256(manifest.encode()).hexdigest())
