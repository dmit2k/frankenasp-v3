#!/usr/bin/env python3
"""C parser tests against independent, immutable Python-generated vector bytes."""
import hashlib,pathlib,struct,subprocess,sys
exe=str(pathlib.Path(sys.argv[1]).resolve())
root=pathlib.Path(__file__).resolve().parent.parent/"testdata"
def call(data=b"",*args):return subprocess.run([exe,*args],input=data,capture_output=True)
def ensure(ok,msg):
 if not ok:raise AssertionError(msg)
lines=(root/"SHA256SUMS").read_text().splitlines()
for line in lines:
 sha,name=line.split("  ",1)
 ensure(hashlib.sha256((root/name).read_bytes()).hexdigest()==sha,name)
ensure((root/"readiness.bin").read_bytes()==b"\xa5","READY fixture")
ensure(call(b"","--ready").stdout==b"A5\n","C READY")
print("manifest",len(lines),"readiness exact-byte PASS")
vectors=[p for p in sorted(root.glob("*.bin")) if p.name!="readiness.bin"]
for p in vectors:
 result=call(p.read_bytes())
 ensure(result.returncode==0 and result.stdout==p.with_suffix(".expect").read_bytes(),p.name+str(result.stderr))
print("C fixtures",len(vectors),"PASS")
for v in vectors:
 u = call(v.read_bytes(),"--unaligned")
 ensure(u.returncode==0 and u.stdout==v.with_suffix(".expect").read_bytes(),"unaligned "+v.name)
print("unaligned decoder",len(vectors),"PASS")
s=(root/"stereo_s24.bin").read_bytes()
pcm=(root/"stereo_s24.pcm").read_bytes()
ensure(pcm[12:18]==bytes.fromhex("a1b2c3d4e5f6"),"PCM fixture")
out=call(s+pcm,"--sample")
ensure(out.returncode==0 and b"S 2 0 a1b2c3\n" in out.stdout and b"S 2 1 d4e5f6\n" in out.stdout,"stereo REF")
print("stereo L/R byte-exact PASS")
def mut(src,off,raw):
 b=bytearray(src);b[off:off+len(raw)]=raw;return bytes(b)
def u16(n):return struct.pack("<H",n)
def u32(n):return struct.pack("<I",n)
bad={
 "CFG2":mut(s,0,b"CFG2"),"version":mut(s,4,u16(2)),
 "buffers0":mut(s,6,b"\0"),"channels0":mut(s,7,b"\0"),
 "size":mut(s,8,u32(len(s)+1)),"input":mut(s,12,u32(35)),
 "rawsize":mut(s,16,u16(65535)),"buffer_offset":mut(s,24,u32(1)),
 "length0":mut(s,28,u32(0)),"rate0":mut(s,32,u32(0)),
 "frames0":mut(s,36,u32(0)),"stride0":mut(s,40,u32(0)),
 "fmt":mut(s,44,b"\x04"),"valid":mut(s,45,b"\x19"),
 "shift":mut(s,46,b"\x01"),"channelcount":mut(s,47,b"\x07"),
 "first_channel":mut(s,48,u16(1)),"reserved":mut(s,50,u16(1)),
 "channel_buffer":mut(s,52,b"\x01"),"role":mut(s,53,b"\x03"),
 "roleindex":mut(s,54,u16(5)),"sample_offset":mut(s,56,u32(17)),
 "role_index_dup":mut(s,62,u16(0)),
 "raw_name":mut(s,24+28+6*8+3,b"\x00"),
 "upper_bound":mut(s,8,u32(65537)),
 "last_sample":mut(mut(s,28,u32(35)),12,u32(35)),
}
three=(root/"separate_three.bin").read_bytes()
bad.update({
 "gap":mut(three,24+28,u32(9)),
 "overlap":mut(three,24+28,u32(7)),
 "channel_range":mut(three,24+28+24,u16(3)),
 "input_overflow":mut(mut(three,28,u32(0xffffffff)),24+28,u32(0xffffffff)),
 "ref_index_gap":mut(s,24+28+5*8+2,u16(4)),
 "raw_duplicate":mut(s,s.index(b"/proc/idme/miccal.1")+len("/proc/idme/miccal."),b"0"),
 "raw_value_too_long":mut((root/"large_factory.bin").read_bytes(),24+28+8+1,u16(32769)),
})
for n in (0,1,4,8,23,24,len(s)//2,len(s)-1):bad["truncated"+str(n)]=s[:n]
for name,data in bad.items():ensure(call(data).returncode!=0,"invalid accepted "+name)
print("invalid/rejected",len(bad),"PASS")
