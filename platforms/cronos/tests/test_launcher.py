#!/usr/bin/env python3
"""Execute the production alias inventory policy without privileged mounts."""
import pathlib
import subprocess
import tempfile

ROOT = pathlib.Path(__file__).resolve().parents[1]
probe = ROOT/'build/test_alias_policy'
with tempfile.TemporaryDirectory() as tmp:
    root = pathlib.Path(tmp)
    for ids in ([], [0, 1], [0, 1, 2, 3], [0, 7, 255], list(range(8))):
        d = root/('case-'+str(len(list(root.iterdir()))))
        d.mkdir()
        for i in ids:
            (d/f'miccal.{i}').symlink_to(f'/proc/device-tree/idme/miccal.{i}/value')
        got = subprocess.run([str(probe), str(d)], capture_output=True)
        assert got.returncode == 0 and got.stdout.decode().strip() == ' '.join(map(str, [len(ids), *ids]))
    for name, target in [('miccal.01', '/proc/device-tree/idme/miccal.1/value'),
                         ('miccal.256', '/proc/device-tree/idme/miccal.256/value'),
                         ('miccal.1', '/etc/passwd'), ('other', '/proc/self')]:
        d = root/('bad-'+str(len(list(root.iterdir())))); d.mkdir()
        (d/name).symlink_to(target)
        assert subprocess.run([str(probe), str(d)], capture_output=True).returncode != 0
    d = root/'regular'; d.mkdir(); (d/'miccal.0').write_bytes(b'test-only')
    assert subprocess.run([str(probe), str(d)], capture_output=True).returncode != 0
    d = root/'too-many'; d.mkdir()
    for i in range(65):
        (d/f'miccal.{i}').symlink_to(f'/proc/device-tree/idme/miccal.{i}/value')
    assert subprocess.run([str(probe), str(d)], capture_output=True).returncode != 0
source = (ROOT/'launcher/launcher.c').read_text()
for invariant in ('same_inode(3, self)', 'checked(DEPLOY_FD, 1, 1, 0)',
                  'MS_REC | MS_PRIVATE', 'CLOSE_RANGE_CLOEXEC',
                  'execveat(child, "", child_argv, child_env, AT_EMPTY_PATH)',
                  'asp_alias_inventory(idme, miccal_ids, &miccal_count)',
                  'link_is(idme, name, target)', 'nonempty miccal sysfs type'):
    assert invariant in source
assert 'i < 4' not in source
assert 'coefs_FBF.cfg' not in source
assert 'LD_LIBRARY_PATH=/system/lib:/vendor/lib' in source
vendor = (ROOT/'src/vendor_factory.c').read_text()
assert 'asp_parameterized_init' in vendor and 'asp_set_ext_ref_sync_status' not in vendor
assert '/system/lib/libasp.so' in vendor
print('CRONOS LAUNCHER PASS: compiled inventory, sparse/native aliases, hostile entry rejection; no mounts')
