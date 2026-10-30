"""Run the existing data2 LAM service sequentially on three attributed sources."""
import hashlib
import json
import os
import shutil
import struct
import subprocess
import time
import zipfile
from pathlib import Path

from gradio_client import Client

ROOT = Path('/data2/huangmr/code/avatar-harness')
EXP = ROOT / 'experiments/lam-musk-20261030'
MOTION = ROOT / 'upstream/LAM/assets/sample_motion/export/I_Am_Iron_Man/I_Am_Iron_Man.mp4'
records = []


def save():
    pending = EXP / 'results.pending.json'
    pending.write_text(json.dumps(records, ensure_ascii=False, indent=2) + '\n')
    pending.replace(EXP / 'results.json')


def validate_avatar(path):
    with zipfile.ZipFile(path) as z:
        bad = z.testzip()
        if bad:
            raise ValueError(f'Corrupt ZIP member: {bad}')
        members = z.namelist()
        required = ['offset.ply', 'skin.glb', 'animation.glb']
        for name in required:
            if not any(x.endswith('/' + name) or x == name for x in members):
                raise ValueError(f'Missing {name}')

        def gltf(name):
            data = z.read(next(x for x in members if x.endswith(name)))
            if data[:4] != b'glTF':
                raise ValueError(f'Invalid GLB: {name}')
            length, kind = struct.unpack_from('<II', data, 12)
            if kind != 0x4E4F534A:
                raise ValueError('First GLB chunk is not JSON')
            return json.loads(data[20:20 + length])

        skin = gltf('skin.glb')
        morphs = sorted({n for m in skin.get('meshes', []) for n in m.get('extras', {}).get('targetNames', [])})
        brows = ['browDownLeft', 'browDownRight', 'browInnerUp', 'browOuterUpLeft', 'browOuterUpRight']
        if not set(brows).issubset(morphs):
            raise ValueError('Missing brow morphs')
        clips = [a.get('name') for a in gltf('animation.glb').get('animations', [])]
        if len(clips) != 7 or 'idle' not in clips[0].lower():
            raise ValueError(f'Unexpected animation order: {clips}')
        return {'zip_bytes': path.stat().st_size, 'zip_sha256': hashlib.sha256(path.read_bytes()).hexdigest(),
                'morph_count': len(morphs), 'brows': brows, 'animation_clips': clips}


for source in json.loads((EXP / 'sources.json').read_text()):
    record = {'id': source['id'], 'status': 'running', 'source_page': source['source_page'], 'motion': str(MOTION)}
    records.append(record)
    save()
    print('START', source['id'], flush=True)
    started = time.monotonic()
    try:
        if shutil.disk_usage(ROOT).free < 2 * 1024**3:
            raise RuntimeError('Less than 2 GiB disk free')
        free = int(subprocess.check_output(['nvidia-smi', '-i', '1', '--query-gpu=memory.free', '--format=csv,noheader,nounits'], text=True).strip())
        if free < 6000:
            raise RuntimeError(f'GPU 1 has only {free} MiB free')
        image = EXP / 'inputs' / source['filename']
        if hashlib.sha256(image.read_bytes()).hexdigest() != source['sha256']:
            raise RuntimeError('Input checksum mismatch')
        output = EXP / 'outputs' / source['id']
        output.mkdir(parents=True, exist_ok=True)
        client = Client('http://127.0.0.1:18767', output_dir=output / 'downloads', verbose=False)
        client.predict(fn_index=3)
        # Gradio 3's video serializer sends its input as the uploaded name.
        # The LAM UI resolves the motion directory from that basename.
        os.chdir(MOTION.parent)
        result = client.predict(str(image), MOTION.name, True, fn_index=4)
        record['seconds'] = round(time.monotonic() - started, 3)
        record['service_outputs'] = [str(x) for x in result]
        for src, filename in zip(result, ['processed.png', 'preview.mp4', 'avatar.zip']):
            if not src or not Path(src).is_file():
                raise RuntimeError(f'Missing generated file: {src}')
            shutil.copy2(src, output / filename)
        record.update(validate_avatar(output / 'avatar.zip'))
        record['output_dir'] = str(output)
        record['status'] = 'ok'
        print('DONE', source['id'], record['seconds'], record['zip_bytes'], flush=True)
    except Exception as exc:
        record.update(status='error', error=str(exc), seconds=round(time.monotonic() - started, 3))
        print('ERROR', source['id'], repr(exc), flush=True)
    save()

print('FINISHED', [r['status'] for r in records], flush=True)
raise SystemExit(0 if all(r['status'] == 'ok' for r in records) else 1)
