#!/usr/bin/env python3
"""PCM-only C conformance: independent exact bytes, malformed/legacy input, sanitizer rejection."""
import hashlib
import pathlib
import struct
import subprocess
import sys
exe=str(pathlib.Path(sys.argv[1]).resolve())
root=pathlib.Path(__file__).resolve().parent.parent.parent/"rook"/"testdata"
def call(data=b"",*args):
    return subprocess.run([exe,*args],input=data,capture_output=True)
def check(ok,msg):
    if not ok: raise AssertionError(msg)
for line in (root/"SHA256SUMS").read_text().splitlines():
    sha,name=line.split("  ",1)
    check(hashlib.sha256((root/name).read_bytes()).hexdigest()==sha,"manifest "+name)
check((root/"readiness.bin").read_bytes()==b"\xa5","1-byte READY")
check(call(b"","--ready").stdout==b"A5\n","C readiness")
vectors=sorted(p for p in root.glob("*.bin") if p.name!="readiness.bin")
for path in vectors:
    wire=path.read_bytes()
    n,m=wire[6],wire[7]
    check(len(wire)==16+28*n+8*m==int.from_bytes(wire[8:12],"little"),"size "+path.name)
    oracle=path.with_suffix(".expect").read_bytes()
    for args in ((),("--unaligned",),("--strict",)):
        got=call(wire,*args)
        check(got.returncode==0 and got.stdout==oracle,"C oracle "+path.name+str(args)+repr(got.stderr))
check((root/"max_255.bin").stat().st_size==9196,"maximum 9196")
print("C01/C02/C03: PCM-only exact fixtures",len(vectors),"unaligned/strict/readiness PASS")
s=(root/"stereo_s24.bin").read_bytes()
pcm=(root/"stereo_s24.pcm").read_bytes()
check(pcm[12:18]==bytes.fromhex("a1b2c3d4e5f6"),"REF fixture")
res=call(s+pcm,"--sample")
check(res.returncode==0 and b"S 2 0 a1b2c3\n" in res.stdout and b"S 2 1 d4e5f6\n" in res.stdout,"stereo REF")
check(call(s+b"\x7f","--strict").returncode==2,"trailing descriptor must reject")
# In normal --sample IPC, following PCM is not part of descriptor and remains readable.
for stem in ("stereo_s24","separate_three"):
    pcm_wire=(root/(stem+".bin")).read_bytes()
    pcm=(root/(stem+".pcm")).read_bytes()
    bounced=call(pcm_wire+pcm,"--roundtrip")
    check(bounced.returncode==0 and bounced.stdout==pcm,"physical input byte-exact "+stem)
print("C05: PCM REF L/R and all physical input bytes byte-exact, trailing strict rejection PASS")

def mut(src,off,payload):
    b=bytearray(src)
    b[off:off+len(payload)]=payload
    return bytes(b)
def u16(x):return struct.pack("<H",x)
def u32(x):return struct.pack("<I",x)
# Old Stage02 24-byte header, same ASP1/version1 with raw sections: no dual reader.
old24=mut(s,8,u32(len(s)+8))
old24=old24[:16]+b"\x00"*8+old24[16:]
bad={
 "old24":old24,
 "CFG2":mut(s,0,b"CFG2"),
 "version":mut(s,4,u16(2)),
 "buffer0":mut(s,6,b"\x00"),
 "channel0":mut(s,7,b"\x00"),
 "size_plus8":mut(s,8,u32(len(s)+8)),
 "size_minus8":mut(s,8,u32(len(s)-8)),
 "size_over_9196":mut(s,8,u32(9197)),
 "input_wrong":mut(s,12,u32(35)),
 "buffer_offset":mut(s,16,u32(1)),
 "buffer_length0":mut(s,20,u32(0)),
 "rate0":mut(s,24,u32(0)),
 "frames0":mut(s,28,u32(0)),
 "stride0":mut(s,32,u32(0)),
 "format":mut(s,36,b"\x04"),
 "valid_bits":mut(s,37,b"\x19"),
 "shift":mut(s,38,b"\x01"),
 "channels":mut(s,39,b"\x07"),
 "first_channel":mut(s,40,u16(1)),
 "reserved":mut(s,42,u16(1)),
 "channel_buffer":mut(s,44,b"\x01"),
 "role":mut(s,45,b"\x03"),
 "role_index":mut(s,46,u16(5)),
 "channel_sample_offset":mut(s,48,u32(17)),
 "role_dup":mut(s,44+8+2,u16(0)),
 "ref_gap":mut(s,44+5*8+2,u16(4)),
 "last_sample":mut(mut(s,20,u32(35)),12,u32(35)),
 "metadata_trailing":mut(s,8,u32(len(s)+4))+b"ABCD",
 "zero_mic":mut(s,45,b"\x02"),
}
three=(root/"separate_three.bin").read_bytes()
bad.update({
 "buffer_gap":mut(three,16+28,u32(9)),
 "buffer_overlap":mut(three,16+28,u32(7)),
 "first_channel_gap":mut(three,16+28+24,u16(3)),
 "input_overflow":mut(mut(three,16+4,u32(0xffffffff)),16+28,u32(0xffffffff)),
})
# Bounds and stream truncation, especially partial short header and records.
for n in (0,1,4,8,15,16,17,43,44,len(s)//2,len(s)-1):
    bad["truncated_"+str(n)]=s[:n]
for name,wire in bad.items():
    got=call(wire)
    check(got.returncode==2 and b"AddressSanitizer" not in got.stderr and
          b"runtime error:" not in got.stderr,
          "malformed accepted/crashed "+name+" rc="+str(got.returncode)+" stderr="+repr(got.stderr))
print("C04/C06: malformed/truncated/old24",len(bad),"rejected with exit=2")
print("C01-C07 component conformance PASS")
