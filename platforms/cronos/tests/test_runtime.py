#!/usr/bin/env python3
"""Independent packed24 stream oracle and process-level lifecycle checks."""
import os
import pathlib
import struct
import subprocess
import sys
import tempfile

ROOT = pathlib.Path(__file__).resolve().parents[1]
CHILD = pathlib.Path(sys.argv[1]).resolve()
P = struct.pack

def descriptor(rate=16000, frames=320, fmt=2, stride=12, shift=0, swapped=False):
    length = frames * stride
    channels = [(0, 1, 0, 0), (0, 1, 1, 3), (0, 2, 0, 6), (0, 2, 1, 9)]
    if swapped:
        channels[0], channels[1] = channels[1], channels[0]
    return (P('<4sHBBII', b'ASP1', 1, 1, 4, 76, length)
            + P('<IIIIIBBBBHH', 0, length, rate, frames, stride, fmt, 24, shift, 4, 0, 0)
            + b''.join(P('<BBHI', *c) for c in channels))

def pack24(value):
    return (value & 0xffffff).to_bytes(3, 'little')

def sample24(frame, ch):
    edges = (0, 1, 255, 256, 0x7fffff, 0x800000, 0xffff00, 0xffffff)
    if frame % 37 < 8:
        return edges[(frame + ch) % 8]
    return (frame * 104729 + ch * 99991 + 0x123457) & 0xffffff

def block(period):
    return b''.join(pack24(sample24(period * 320 + f, ch))
                    for f in range(320) for ch in range(4))

def case(desc, blocks=(), fault=None, sequence=False, fragment=False, inject=False, resource=True):
    with tempfile.TemporaryDirectory() as tmp:
        sentinel = pathlib.Path(tmp) / 'test-resource'
        if resource:
            sentinel.write_bytes(b'test-only')
        env = os.environ.copy()
        env['ASP1_TEST_RESOURCE'] = str(sentinel)
        for name in ('ASP1_TEST_FAULT', 'ASP1_TEST_SEQUENCE', 'LD_PRELOAD'):
            env.pop(name, None)
        if fault:
            env['ASP1_TEST_FAULT'] = fault
        if sequence:
            env['ASP1_TEST_SEQUENCE'] = '1'
        if inject:
            env['LD_PRELOAD'] = str(ROOT / 'build/io_faults.so')
        p = subprocess.Popen([str(CHILD)], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                             stderr=subprocess.PIPE, env=env)
        data = desc + b''.join(blocks)
        if fragment:
            for i in range(0, len(data), 7):
                p.stdin.write(data[i:i+7])
                p.stdin.flush()
            p.stdin.close()
            out, err = p.stdout.read(), p.stderr.read()
            p.wait(timeout=15)
        else:
            out, err = p.communicate(data, timeout=15)
        return p.returncode, out, err.decode('utf-8', 'replace')

def require(value, message):
    if not value:
        raise AssertionError(message)

good = descriptor()
require(len(good) == 76, 'descriptor size')
for periods in (0, 1, 2, 3, 311):
    rc, output, err = case(good, [block(i) for i in range(periods)], sequence=True)
    require(rc == 0 and output[:1] == b'\xa5' and len(output) == 1 + periods*640, 'wire geometry')
    actual = struct.unpack('<' + str(periods*320) + 'h', output[1:])
    expected = ([0]*64 + [(i % 30001)-15000 for i in range((periods*320)//128*128)])[:periods*320]
    require(list(actual) == expected, 'single initial pad and exact sample order')
    require('mock vendor stdout chatter' in err, 'vendor stdout isolated')
    require(f'process={(periods*320)//128} destroy=1 deinit=1' in err, 'quantum count and cleanup')
for mutation in ('swap', 'duplicate', 'lost'):
    b = bytearray(block(0))
    if mutation == 'swap':
        b[0:3], b[3:6] = b[3:6], b[0:3]
    elif mutation == 'duplicate':
        b[12:24] = b[0:12]
    else:
        b[12*17:12*18] = b[12*18:12*19]
    rc, out, _ = case(good, [b], sequence=True)
    require(rc != 0 and out == b'\xa5', 'oracle detects PCM mutation: '+mutation)
for kwargs in (dict(rate=48000), dict(frames=128), dict(fmt=3), dict(stride=15),
               dict(shift=1), dict(swapped=True)):
    rc, out, _ = case(descriptor(**kwargs))
    require(rc != 0 and not out, 'invalid native geometry')
for bad in (good[:-1], good[:20], b'CFG2'+good[4:], good[:8]+P('<I', 77)+good[12:],
            good[:48]+P('<I', 12)+good[52:]):
    rc, out, _ = case(bad)
    require(rc != 0 and not out, 'invalid descriptor')
rc, out, _ = case(good, [block(0), block(1)], sequence=True, fragment=True)
require(rc == 0 and len(out) == 1281, 'short fragmented writes')
if 'san' not in str(CHILD):
    rc, out, _ = case(good, [block(0), block(1)], sequence=True, inject=True)
    require(rc == 0 and len(out) == 1281, 'EINTR and short syscall injection')
rc, out, _ = case(good, [block(0)[:111]])
require(rc != 0 and out == b'\xa5', 'truncated period')
rc, out, _ = case(good, resource=False)
require(rc != 0 and not out, 'unavailable resource before READY')
for fault in ('load', 'init', 'create', 'process', 'counts', 'input-count',
              'overrun', 'far-overrun', 'mutate', 'input-overrun', 'destroy'):
    before_ready = fault in ('load', 'init', 'create')
    rc, out, err = case(good, [] if before_ready else [block(0)], fault=fault)
    require(rc != 0 and bool(out[:1] == b'\xa5') != before_ready, 'fault result '+fault)
    if not before_ready:
        require('destroy=1 deinit=1' in err, 'failure cleanup '+fault)
    if fault == 'create':
        require('destroy=0 deinit=1' in err, 'failed create cleanup')
# Zero MIC periods remain ordinary PCM; nonzero REF is retained without a reset.
muted = b''.join(pack24(x) for x in (0, 0, 8388607, -8388608)) * 320
rc, out, err = case(good, [block(0)]*4 + [muted]*4 + [block(0)]*4)
require(rc == 0 and len(out) == 1+12*640 and 'process=30' in err, 'no mute reset/re-padding')
with tempfile.TemporaryDirectory() as tmp:
    sentinel = pathlib.Path(tmp)/'resource'
    sentinel.write_bytes(b'test')
    env = os.environ.copy()
    env['ASP1_TEST_RESOURCE'] = str(sentinel)
    env.pop('ASP1_TEST_FAULT', None)
    env.pop('ASP1_TEST_SEQUENCE', None)
    p = subprocess.Popen([str(CHILD)], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                         stderr=subprocess.PIPE, env=env)
    p.stdin.write(good); p.stdin.flush()
    require(p.stdout.read(1) == b'\xa5', 'READY before broken pipe')
    p.stdout.close()
    try:
        p.stdin.write(block(0)); p.stdin.flush()
    except BrokenPipeError:
        pass
    p.stdin.close()
    p.wait(timeout=10)
    require(p.returncode != 0, 'broken stdout is terminal')
    p.stderr.close()
print('CRONOS RUNTIME PASS: signed24/all channels, 311 periods/777 quanta, FIFO, I/O, faults, cleanup')
