#!/usr/bin/env python3
"""Add the selected Colorado avatar's connected eyeball splat ranges.
No geometry, appearance or animations are changed. Original must match SHA256.
"""
import hashlib
import json
import sys
import zipfile
from pathlib import Path

path = Path(sys.argv[1])
expected = '5da20bf3ebde99f2cf2bbc1934674e1f41204bbb13970f0b7b3eadfc269ba3bb'
config = {'left_iris':[[3931,4476],[16754,18385]],'right_iris':[[4477,5022],[18386,20017]]}
with zipfile.ZipFile(path) as archive:
    if any(name.endswith('/iris_occlusion.json') for name in archive.namelist()):
        print('Already has iris occlusion'); sys.exit(0)
    if hashlib.sha256(path.read_bytes()).hexdigest() != expected:
        raise SystemExit('Asset mismatch: do not apply another avatar\'s splat ranges')
    root = next(name.rsplit('/',1)[0] for name in archive.namelist() if name.endswith('/skin.glb'))
    members = [(info,archive.read(info.filename)) for info in archive.infolist()]
temp = path.with_suffix('.occlusion.tmp')
with zipfile.ZipFile(temp,'w',compression=zipfile.ZIP_DEFLATED) as archive:
    for info,data in members: archive.writestr(info,data)
    archive.writestr(root+'/iris_occlusion.json',json.dumps(config,separators=(',',':')))
temp.replace(path)
print(hashlib.sha256(path.read_bytes()).hexdigest())
