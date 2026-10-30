"""Download public ModelScope files with per-file revision and SHA256 checks."""
import argparse
import concurrent.futures
import hashlib
import json
import os
from pathlib import Path
import shutil
import time
from urllib.parse import quote

import requests


def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as source:
        for chunk in iter(lambda: source.read(4 * 1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('repo')
    parser.add_argument('destination', type=Path)
    parser.add_argument('--workers', type=int, default=2)
    args = parser.parse_args()
    root = args.destination.resolve()
    root.mkdir(parents=True, exist_ok=True)
    response = requests.get('https://modelscope.cn/api/v1/models/' + args.repo + '/repo/files',
                            params={'Revision': 'master', 'Recursive': 'true'}, timeout=(20, 60))
    response.raise_for_status()
    metadata = response.json()
    if metadata.get('Code') != 200:
        raise RuntimeError('ModelScope repository metadata unavailable')
    files = [f for f in metadata['Data']['Files'] if f['Type'] == 'blob']
    (root / 'download-manifest.json').write_text(json.dumps({'repo': args.repo, 'files': files}, indent=2))

    def download(item):
        path = root / item['Path']
        if root not in path.resolve().parents:
            raise RuntimeError('Unexpected path in repository')
        path.parent.mkdir(parents=True, exist_ok=True)
        if path.exists() and path.stat().st_size == item['Size'] and digest(path) == item['Sha256']:
            return item['Path'] + ' verified (cached)'
        partial = path.with_name(path.name + '.incomplete')
        url = 'https://modelscope.cn/models/' + args.repo + '/resolve/' + item['Revision'] + '/' + quote(item['Path'], safe='/')
        for attempt in range(5):
            offset = partial.stat().st_size if partial.exists() else 0
            if shutil.disk_usage(root).free < max(0, item['Size'] - offset) + 3 * 1024**3:
                raise RuntimeError('Insufficient deployment disk space')
            try:
                with requests.get(url, headers={'Range': 'bytes=%d-' % offset} if offset else {},
                                  stream=True, timeout=(20, 120)) as stream:
                    stream.raise_for_status()
                    append = offset > 0 and stream.status_code == 206
                    with partial.open('ab' if append else 'wb') as target:
                        for chunk in stream.iter_content(4 * 1024 * 1024):
                            if chunk:
                                target.write(chunk)
                if partial.stat().st_size != item['Size'] or digest(partial) != item['Sha256']:
                    raise RuntimeError('File size or SHA256 mismatch: ' + item['Path'])
                os.replace(partial, path)
                return item['Path'] + ' verified'
            except requests.RequestException as exc:
                # Signed redirect URLs can contain credentials; log only the exception type.
                print(item['Path'], 'retry', attempt + 1, type(exc).__name__, flush=True)
                time.sleep(min(10, 2 ** attempt))
        raise RuntimeError('Download retries exhausted: ' + item['Path'])

    with concurrent.futures.ThreadPoolExecutor(max_workers=args.workers) as pool:
        for result in pool.map(download, sorted(files, key=lambda x: -x['Size'])):
            print(result, flush=True)


if __name__ == '__main__':
    main()
