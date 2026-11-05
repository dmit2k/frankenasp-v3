#!/usr/bin/env python3
"""Nonprivileged offline security contract tests; no mount/chroot or device use."""
import pathlib, tempfile, os, re
ROOT=pathlib.Path(__file__).resolve().parents[1]
launcher=(ROOT/'launcher/launcher.c').read_text()
child=(ROOT/'src/child.c').read_text()
vendor=(ROOT/'src/vendor_factory.c').read_text()
assert 'same_inode(3, self)' in launcher and 'checked(DEPLOY_FD, 1, 1, 0)' in launcher
assert 'O_NOFOLLOW' in launcher and 'openat(parent, name, flags)' in launcher
assert 'p.p_type == PT_INTERP' not in launcher
assert 'CLONE_NEWNS' in launcher and 'MS_REC | MS_PRIVATE' in launcher
assert 'MS_BIND | MS_REMOUNT | RO_FLAGS' in launcher
assert 'execveat(child, "", child_argv, child_env, AT_EMPTY_PATH)' in launcher
assert 'close_range' in launcher and 'CLOSE_RANGE_CLOEXEC' in launcher
policy=(ROOT/'launcher/alias_policy.c').read_text()
assert 'asp_alias_paths((unsigned)i, name, target)' in launcher
assert '/proc/device-tree/idme/miccal.%u/value' in policy
assert 'link_is(idme, name, target)' in launcher
assert 'authentic miccal path unavailable' in launcher
assert 'source_pair=OWNER_VERIFIED' in launcher
assert 'component(factory, "channel-map"' not in launcher
assert 'libasp.so' in launcher and 'factory_sanity(vendor, system)' in launcher
assert 'root ownership/mode' in launcher and 'private directory mode' in launcher
assert 'require(!live, "rook requires matched bundled system/vendor")' in launcher
assert 'getrlimit(RLIMIT_NOFILE' in launcher and 'F_SETFD, FD_CLOEXEC' in launcher
assert 'coefs_FBF.cfg' in launcher and 'asp.cfg' not in launcher
assert 'LD_LIBRARY_PATH=/system/lib:/vendor/lib' in launcher
assert 'asp_vendor_load(&abi)' in child and 'asp_write_exact(wire, &ready, 1)' in child
assert child.index('asp_runtime_open(&runtime, &abi)') < child.index('asp_write_exact(wire, &ready, 1)')
assert 'FACTORY_PATH "/system/lib/libasp.so"' in vendor
assert 'file_sha256(FACTORY_PATH,digest)' in vendor
assert 'base+0x26e25u' in vendor and 'base+0x26ef9u' in vendor
assert 'expected_vtable=base+0x1aa448u' in vendor
for f in [launcher,child,vendor,(ROOT/'src/runtime.c').read_text()]:
 assert not re.search(r'CFG2|TLV|NotReady|ACK2|RSP2|INP2|Q30|calibration.?gain.?multiply|OTA|installer',f,re.I), 'obsolete metadata/control'
with tempfile.TemporaryDirectory() as t:
 root=pathlib.Path(t)
 (root/'sys/firmware/devicetree/base/idme').mkdir(parents=True)
 (root/'proc/idme').mkdir(parents=True)
 (root/'proc/device-tree').symlink_to('/sys/firmware/devicetree/base')
 for i in range(4):
  dst=root/f'sys/firmware/devicetree/base/idme/miccal.{i}/value'
  dst.parent.mkdir(parents=True)
  dst.write_bytes(bytes([33+i]))
  alias=root/f'proc/idme/miccal.{i}'
  alias.symlink_to(f'/proc/device-tree/idme/miccal.{i}/value')
  assert os.readlink(alias)==f'/proc/device-tree/idme/miccal.{i}/value'
  assert dst.is_file()
 (root/'sys/firmware/devicetree/base/idme/miccal.2/value').unlink()
 assert not (root/'sys/firmware/devicetree/base/idme/miccal.2/value').exists()
print('H02 H08 PASS: offline alias graph, real source path policy, missing source and static trust checks (no mounts)')
