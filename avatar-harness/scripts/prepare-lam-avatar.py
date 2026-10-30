#!/usr/bin/env python3
"""Keep LAM geometry intact; select/reorder supplied clips for the web renderer."""
import argparse
import json
import struct
import zipfile
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument('source', type=Path)
parser.add_argument('output', type=Path)
args = parser.parse_args()
if args.source.resolve() == args.output.resolve():
    parser.error('output must differ from the source archive')
names = ['yumi_h5_a3_idle', 'yumi_h5_a3_speak_1', 'yumi_h5_a3_speak_2',
         'yumi_h5_a3_speak_3', 'yumi_h5_a3_speak_4', 'yumi_h5_a3_speak', 'yumi_h5_a3_speak_end']
args.output.parent.mkdir(parents=True, exist_ok=True)
with zipfile.ZipFile(args.source) as source, zipfile.ZipFile(args.output, 'w', zipfile.ZIP_DEFLATED) as output:
    for info in source.infolist():
        data = source.read(info.filename)
        if info.filename.endswith('/animation.glb'):
            size = struct.unpack_from('<I', data, 12)[0]
            doc = json.loads(data[20:20 + size])
            clips = {clip['name']: clip for clip in doc['animations']}
            if all(name in clips for name in names):
                doc['animations'] = [clips[name] for name in names]
                encoded = json.dumps(doc, separators=(',', ':')).encode()
                encoded += b' ' * (-len(encoded) % 4)
                tail = data[20 + size:]
                data = (struct.pack('<III', 0x46546C67, 2, 20 + len(encoded) + len(tail))
                        + struct.pack('<II', len(encoded), 0x4E4F534A) + encoded + tail)
        output.writestr(info, data)
print(args.output)
