#!/usr/bin/env python3
"""Process-level ASP1 tests with independent Rook factory ABI mock."""
import os
import pathlib
import struct
import subprocess
import sys
import tempfile
ROOT = pathlib.Path(__file__).resolve().parents[1]
CHILD = ROOT / sys.argv[1]
P = struct.pack
def descriptor(rate=16000, frames=320, fmt=2, stride=18, shift=0,
               channel_count=6, ref=True, bad_order=False, buffer_count=1):
    channels = [(0, 1, i, i * 3) for i in range(4)]
    if ref:
        channels += [(0, 2, i, (i + 4) * 3) for i in range(2)]
    if bad_order:
        channels[0], channels[1] = channels[1], channels[0]
    if channel_count != len(channels):
        channels = channels[:channel_count]
    length = frames * stride
    buffers = [(0, length, rate, frames, stride, fmt, 24, shift,
                len(channels), 0, 0)] * buffer_count
    head = P('<4sHBBII', b'ASP1', 1, len(buffers), len(channels),
             16 + 28 * len(buffers) + 8 * len(channels), length * buffer_count)
    return head + b''.join(P('<IIIIIBBBBHH', *b) for b in buffers) + (
        b''.join(P('<BBHI', *c) for c in channels))
def pack24(v):
    return (v & 0xffffff).to_bytes(3, 'little')
def block(mic=(1024, -12345, 8388607, -8388608), ref=(111, -222)):
    frame = b''.join(pack24(x) for x in mic)
    frame += b''.join(pack24(x * 256) for x in ref)
    return frame * 320
def sample24(frame, ch):
    edge = (0, 1, 255, 256, 0x7fffff, 0x800000, 0xffff00, 0xffffff)
    if frame % 37 < 8:
        return edge[(frame + ch) % 8]
    return (frame * 104729 + ch * 99991 + 0x123457) & 0xffffff
def sequence_block(exchange):
    return b''.join(pack24(sample24(exchange * 320 + f, ch))
                    for f in range(320) for ch in range(6))
def case(desc, blocks=(), fault=None, resource=True, fragment=False, inject=False,
         sequence=False):
    with tempfile.TemporaryDirectory(prefix='rook-asp1-test-') as tmp:
        path = pathlib.Path(tmp) / 'factory-resource'
        if resource:
            path.write_bytes(b'mock runtime sentinel')
        env = os.environ.copy()
        env['ASP1_TEST_RESOURCE'] = str(path)
        env.pop('ASP1_TEST_SEQUENCE', None)
        if sequence:
            env['ASP1_TEST_SEQUENCE'] = '1'
        if fault:
            env['ASP1_TEST_FAULT'] = fault
        else:
            env.pop('ASP1_TEST_FAULT', None)
        if inject:
            env['LD_PRELOAD'] = str(ROOT / 'build/io_faults.so')
        proc = subprocess.Popen([str(CHILD)], stdin=subprocess.PIPE,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env)
        if fragment:
            # Deliver the descriptor and PCM in short writes.
            data = desc + b''.join(blocks)
            try:
                for i in range(0, len(data), 7):
                    proc.stdin.write(data[i:i + 7])
                    proc.stdin.flush()
            except BrokenPipeError:
                pass
            proc.stdin.close()
            out = proc.stdout.read()
            err = proc.stderr.read()
            proc.wait(timeout=15)
        else:
            out, err = proc.communicate(desc + b''.join(blocks), timeout=15)
        return proc.returncode, out, err.decode('utf-8', 'replace')
def must(yes, what):
    if not yes:
        raise AssertionError(what)
good = descriptor()
must(len(good) == 92, 'exact 92-byte ASP1 descriptor')
n = 311  # 777 factory quanta, includes multiple timestamp second boundaries.
rc, out, err = case(good, [sequence_block(i) for i in range(n)], sequence=True)
must(rc == 0 and len(out) == 1 + 640 * n and out[0] == 0xa5, 'READY/output length')
samples = struct.unpack('<' + str(320*n) + 'h', out[1:])
expected = [0]*64 + [101 + q%1000 for q in range((320*n)//128) for _ in range(128)]
must(list(samples) == expected[:320*n], 'exact 320-to-128 accumulator/FIFO continuity, pad once')
must('mock vendor stdout chatter' in err and b'mock' not in out, 'stdout isolation')
quantum = (320*n)//128
must(f'push={quantum} process={quantum} unregister=1 destroy=1 deinit=1' in err,
     'native writer and factory exactly once, ordered sequential quanta')
# Mutations in the independently generated stream must be terminal.
for change in ('swapped-channels', 'duplicated-frame', 'lost-frame'):
    bad = bytearray(sequence_block(0))
    if change == 'swapped-channels':
        bad[0:3], bad[3:6] = bad[3:6], bad[0:3]
    elif change == 'duplicated-frame':
        bad[18:36] = bad[0:18]
    else:
        bad[17*18:18*18] = bad[18*18:19*18]
    rc, corrupt, _ = case(good, [bytes(bad)], sequence=True)
    must(rc != 0 and corrupt == b'\xa5', 'PCM order mutation rejection: ' + change)
for fault in ('codec-unpack-address', 'codec-pack-offset', 'codec-corrupt'):
    rc, corrupt, _ = case(good, [sequence_block(0)], fault=fault, sequence=True)
    must(rc != 0 and corrupt == b'\xa5', 'codec address/REF oracle fault: ' + fault)
for spec in (dict(rate=48000), dict(frames=128), dict(fmt=3), dict(shift=1),
             dict(stride=21), dict(ref=False), dict(bad_order=True)):
    rc, out, _ = case(descriptor(**spec))
    must(rc != 0 and not out, f'unsupported geometry fails before READY: {spec}')
for d in (good[:-1], good[:8] + P('<I', 93) + good[12:], b'CFG2'+good[4:],
          good[:20], good[:16]+good[16:40]+b'\xff'*4+good[44:]):
    rc, out, _ = case(d)
    must(rc != 0 and not out, 'malformed descriptor fails before READY')
rc, out, err = case(good, [block()]*2, fragment=True)
must(rc == 0 and len(out) == 1281, 'fragmented pipes')
if 'san' not in str(CHILD):
    rc, out, err = case(good, [block()]*2, inject=True)
    must(rc == 0 and len(out) == 1281, 'injected EINTR / short syscalls')
rc, out, _ = case(good, [block()[:111]])
must(rc != 0 and out == b'\xa5', 'truncated PCM terminal after READY')
rc, out, _ = case(good, resource=False)
must(rc != 0 and not out, 'missing factory resource prevents READY')
for fault, ready in [('init',False),('create',False),('ref',False),
                     ('register',False),('push',True),('process',True),
                     ('counts',True),('overrun',True),('mutate',True),
                     ('unregister',True),('destroy',True)]:
    rc,out,err = case(good, [block()] if ready else [], fault=fault)
    must(rc != 0 and (out[:1] == b'\xa5') == ready, 'fault outcome ' + fault)
    if fault in ('push','process','counts','overrun','mutate','unregister','destroy'):
        must('unregister=1 destroy=1 deinit=1' in err, 'failure cleanup '+fault)
    if fault=='register':
        must('unregister=0 destroy=1 deinit=1' in err, 'failed register cleanup')
    if fault=='create':
        must('destroy=0 deinit=1' in err, 'failed create cleanup')
# Muted MIC is just zero PCM. REF and time evolution must continue.
rc, out, err = case(good,[block()]*4 + [block(mic=(0,0,0,0))]*4 + [block()]*4)
must(rc == 0 and len(out)==1+12*640 and 'push=30 process=30' in err,
     'no re-padding, reset, or special mute handling')
# A closed output pipe is terminal, never reported as success.
with tempfile.TemporaryDirectory() as tmp:
    path = pathlib.Path(tmp)/'resource'
    path.write_bytes(b'factory mock')
    env = os.environ.copy()
    env.update(ASP1_TEST_RESOURCE=str(path))
    env.pop('ASP1_TEST_FAULT', None)
    proc=subprocess.Popen([str(CHILD)], stdin=subprocess.PIPE,
                          stdout=subprocess.PIPE,stderr=subprocess.PIPE,env=env)
    proc.stdin.write(good)
    proc.stdin.flush()
    must(proc.stdout.read(1)==b'\xa5','READY before broken pipe')
    proc.stdout.close()
    try:
        proc.stdin.write(block())
        proc.stdin.flush()
    except BrokenPipeError:
        pass
    proc.stdin.close()
    proc.stderr.close()
    must(proc.wait(timeout=10)!=0,'broken stdout pipe is terminal')
print('ROOK RUNTIME PASS: geometry, 311 varying exchanges / 777 quanta, codec addresses, independent REF, PCM mutations, FIFO, timestamps, cleanup, I/O')
