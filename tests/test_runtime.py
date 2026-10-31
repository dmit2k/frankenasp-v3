#!/usr/bin/env python3
"""Process-level tests of compiled codec + fixed wire + real bridge, independent test ABI."""
import os, pathlib, struct, subprocess, sys, tempfile, threading, time
ROOT = pathlib.Path(__file__).resolve().parents[1]
CHILD = ROOT / sys.argv[1]
P = struct.pack
def descriptor(rate=96000,frames=1920,micfmt=3,micshift=0,ref=True):
    channels = [(0,1,i,4*i) for i in range(4)] + ([(1,2,i,2*i) for i in range(2)] if ref else [])
    nb = 2 if ref else 1
    micstride = 16 if micfmt == 3 else 12
    miclen = frames*micstride
    reflen = frames*4
    buffers = [(0,miclen,rate,frames,micstride,micfmt,24,micshift,4,0,0)]
    if ref: buffers.append((miclen,reflen,rate,frames,4,1,16,0,2,4,0))
    total = miclen + (reflen if ref else 0)
    head = P('<4sHBBII',b'ASP1',1,nb,len(channels),16+28*nb+8*len(channels),total)
    recs = b''.join(P('<IIIIIBBBBHH',*r) for r in buffers)
    recs += b''.join(P('<BBHI',*r) for r in channels)
    return head+recs
def block(mic=(1024,2048,4096,8192),ref=(111,-222),high=False):
    return b''.join(P('<4i',*[x << 8 if high else x for x in mic]) for _ in range(1920)) + b''.join(P('<2h',*ref) for _ in range(1920))
def case(desc,blocks=(),maptext="ASP1-CHANNEL-MAP-V1\nmic=0,1,2,3\nref=0,1\n",resource=True,fault=None,fragment=False,inject=False,resource_content=b'resource mock sentinel'):
    with tempfile.TemporaryDirectory(prefix="asp1-test-") as tmp:
        d=pathlib.Path(tmp)
        (d/'map').write_text(maptext)
        if resource: (d/'vendor').write_bytes(resource_content)
        env=os.environ.copy()
        env.update(ASP1_TEST_MAP=str(d/'map'),ASP1_TEST_RESOURCE=str(d/'vendor'))
        if fault: env['ASP1_TEST_FAULT']=fault
        else: env.pop('ASP1_TEST_FAULT',None)
        if inject: env['LD_PRELOAD']=str(ROOT/'build/io_faults.so')
        p=subprocess.Popen([str(CHILD)],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,env=env)
        data=desc+b''.join(blocks)
        if fragment:
            # byte-wise descriptor delivery and chunked PCM; no extra framing.
            for x in range(0,len(data),7):
                try: p.stdin.write(data[x:x+7]);p.stdin.flush()
                except BrokenPipeError: break
            p.stdin.close()
            out=p.stdout.read()
            err=p.stderr.read()
            p.wait(timeout=10)
        else:
            out,err=p.communicate(data,timeout=10)
        return p.returncode,out,err.decode('utf-8','replace')
def must(cond,what):
    if not cond: raise AssertionError(what)
valid=descriptor()
must(len(valid)==120 and valid[:4]==b'ASP1', 'PCM-only 16/28/8')
n=35
rc,out,err=case(valid,[block()]*n)
must(rc==0 and len(out)==1+640*n and out[:1]==b'\xa5','READY and exact output')
must(out[1:129]==b'\0'*128,'64 zero samples once')
must(any(out[129:641]),'first output has 256 vendor samples')
must(all(any(out[1+640*i:1+640*(i+1)]) for i in range(1,n)),'nonzero DSP steady state')
must('mock vendor stdout chatter' in err and b'mock' not in out,'stdout isolation')
must(f'process={n*1920//768}' in err and 'destroy=1 deinit=1' in err,'8ms quanta, cleanup exactly once')
for change in (dict(rate=48000),dict(frames=960),dict(micfmt=2),dict(ref=False)):
    rc,out,err=case(descriptor(**change))
    must(rc!=0 and b'\xa5' not in out and not out,'valid but unsupported before READY '+str(change))
for bad in (valid[:-1],valid[:8]+P('<I',121)+valid[12:],b'CFG2'+valid[4:],valid[:20]):
    rc,out,err=case(bad)
    must(rc!=0 and not out,'malformed/truncated descriptor before READY')
rc,out,err=case(valid,[block()]*2,fragment=True)
must(rc==0 and len(out)==1281,'fragmented syscall I/O')
if 'san' not in str(CHILD):
    rc,out,err=case(valid,[block()]*2,inject=True)
    must(rc==0 and len(out)==1281,'real injected EINTR + short read/write syscall I/O')
rc,out,err=case(valid,[block()[:100]])
must(rc!=0 and out==b'\xa5','truncated PCM terminal after READY')
rc,out,err=case(valid,resource=False)
must(rc!=0 and not out and 'vendor resource' in err,'missing factory resource prevents READY')
for fault,ready in [('init',False),('create',False),('ref',False),('process',True),('counts',True),('overrun',True)]:
    rc,out,err=case(valid,[block()] if ready else [],fault=fault)
    must(rc!=0 and (out[:1]==b'\xa5')==ready,'ABI fault '+fault)
    must('destroy=1 deinit=1' in err if fault in ('process','counts','overrun','ref')
         else 'deinit=1' in err if fault=='create' else 'deinit=0' in err,'ABI cleanup '+fault)
# Distinct REF L/R and mic slot permutations must change actual native input to mock ABI.
_,a,_=case(valid,[block(ref=(111,-222))])
_,b,_=case(valid,[block(ref=(-222,111))])
must(a!=b,'REF stereo L/R markers must remain distinct')
_,a,_=case(valid,[block()])
_,b,_=case(valid,[block()],maptext="ASP1-CHANNEL-MAP-V1\nmic=1,0,2,3\nref=0,1\n")
must(a!=b,'trusted captured-slot mic mapping honored')
_,a,_=case(valid,[block()],resource_content=b'first pretend factory value')
_,b,_=case(valid,[block()],resource_content=b'second pretend factory value')
must(a==b,'no launcher/PCM calibration bytes or duplicate gain application')
_,a,_=case(descriptor(micshift=8),[block(high=True)])
must(a==case(valid,[block()])[1],'high/low 24-bit equivalence')
_,a,_=case(valid,[block()]*4 + [block(mic=(0,0,0,0),ref=(0,0))]*4 + [block()]*4)
must(len(a)==1+640*12 and a[1+640*8:1+640*8+128]!=b'\0'*128,'no initial padding after mute')
rc,out,err=case(valid,maptext="ASP1-CHANNEL-MAP-V1\nmic=0,0,2,3\nref=0,1\n")
must(rc!=0 and not out,'malformed channel map rejected before READY')
with tempfile.TemporaryDirectory(prefix="asp1-broken-pipe-") as tmp:
    d=pathlib.Path(tmp)
    (d/'map').write_text("ASP1-CHANNEL-MAP-V1\nmic=0,1,2,3\nref=0,1\n")
    (d/'vendor').write_bytes(b'mock vendor resource')
    env=os.environ.copy()
    env.update(ASP1_TEST_MAP=str(d/'map'),ASP1_TEST_RESOURCE=str(d/'vendor'))
    env.pop('ASP1_TEST_FAULT',None)
    proc=subprocess.Popen([str(CHILD)],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,env=env)
    proc.stdin.write(valid); proc.stdin.flush()
    must(proc.stdout.read(1)==b'\xa5','broken-pipe test must initially READY')
    proc.stdout.close()
    try:
        proc.stdin.write(block()); proc.stdin.flush()
    except BrokenPipeError:
        pass
    proc.stdin.close()
    proc.stderr.close()
    must(proc.wait(timeout=10)!=0,'closed output pipe must terminate helper, not emit success')
print('H01 H02 H03 H04 H05 PASS: compiled independent ABI mock, PCM-only descriptor, FIFO, isolation, failure/cleanup')
