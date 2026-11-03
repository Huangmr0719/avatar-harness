#!/usr/bin/env python3
"""Prepare LAM-20K dialogue assets, without changing geometry or source files."""
import argparse
import json
import math
import struct
import zipfile
from pathlib import Path

CLIPS = ['yumi_h5_a3_idle', 'yumi_h5_a3_speak_1', 'yumi_h5_a3_speak_2',
         'yumi_h5_a3_speak_3', 'yumi_h5_a3_speak_4', 'yumi_h5_a3_speak', 'yumi_h5_a3_speak_end']
PITCH_DOWN_DEGREES = 6
CALIBRATION = 'harness_calibration.json'
JSON_CHUNK, BIN_CHUNK = 0x4E4F534A, 0x004E4942


def read_glb(data):
    if struct.unpack_from('<III', data) != (0x46546C67, 2, len(data)):
        raise ValueError('Expected a GLB 2.0 file')
    chunks, offset = [], 12
    while offset < len(data):
        size, kind = struct.unpack_from('<II', data, offset)
        end = offset + 8 + size
        if end > len(data):
            raise ValueError('Truncated GLB chunk')
        chunks.append((kind, data[offset + 8:end]))
        offset = end
    if not chunks or chunks[0][0] != JSON_CHUNK:
        raise ValueError('Missing GLB JSON chunk')
    return json.loads(chunks[0][1]), chunks


def write_glb(doc, chunks):
    encoded = json.dumps(doc, separators=(',', ':')).encode()
    encoded += b' ' * (-len(encoded) % 4)
    body = b''.join(struct.pack('<II', len(data), kind) + data
                    for kind, data in [(JSON_CHUNK, encoded)] + chunks[1:])
    return struct.pack('<III', 0x46546C67, 2, 12 + len(body)) + body


def accessor_layout(doc, index):
    accessor = doc['accessors'][index]
    view = doc['bufferViews'][accessor['bufferView']]
    if accessor.get('sparse') or accessor.get('normalized') or view.get('buffer', 0) != 0:
        raise ValueError('Unsupported GLB accessor layout')
    scalar = {5121: 'B', 5123: 'H', 5125: 'I', 5126: 'f'}[accessor['componentType']]
    width = {'SCALAR': 1, 'VEC3': 3, 'VEC4': 4}[accessor['type']]
    fmt = '<' + scalar * width
    start = view.get('byteOffset', 0) + accessor.get('byteOffset', 0)
    stride = view.get('byteStride', struct.calcsize(fmt))
    return accessor, fmt, start, stride


def read_accessor(doc, binary, index):
    accessor, fmt, start, stride = accessor_layout(doc, index)
    return [struct.unpack_from(fmt, binary, start + i * stride) for i in range(accessor['count'])]


def adjust_animation(data, delta_degrees):
    doc, chunks = read_glb(data)
    clips = {clip['name']: clip for clip in doc['animations']}
    if all(name in clips for name in CLIPS):
        doc['animations'] = [clips[name] for name in CLIPS]
    bin_index = next(i for i, (kind, _) in enumerate(chunks) if kind == BIN_CHUNK)
    binary = bytearray(chunks[bin_index][1])
    sine, cosine = math.sin(math.radians(delta_degrees) / 2), math.cos(math.radians(delta_degrees) / 2)
    adjusted = set()
    for clip in doc['animations']:
        channels = [channel for channel in clip['channels']
                    if channel['target']['path'] == 'rotation'
                    and doc['nodes'][channel['target']['node']].get('name') == 'head']
        if len(channels) != 1:
            raise ValueError('Expected one animated head rotation per clip')
        sampler = clip['samplers'][channels[0]['sampler']]
        if sampler.get('interpolation', 'LINEAR') != 'LINEAR':
            raise ValueError('Expected LINEAR head rotation')
        index = sampler['output']
        if index in adjusted:
            continue  # Clips may share their head keys: apply the offset only once.
        adjusted.add(index)
        accessor, fmt, start, stride = accessor_layout(doc, index)
        if accessor['componentType'] != 5126 or accessor['type'] != 'VEC4':
            raise ValueError('Expected float quaternion head keys')
        values = []
        for i in range(accessor['count']):
            offset = start + i * stride
            x, y, z, w = struct.unpack_from(fmt, binary, offset)
            # Local +X right multiplication lowers the LAM head's world gaze.
            q = (x * cosine + w * sine, y * cosine + z * sine,
                 z * cosine - y * sine, w * cosine - x * sine)
            norm = math.sqrt(sum(v * v for v in q))
            if not math.isfinite(norm) or norm == 0:
                raise ValueError('Invalid head quaternion')
            q = tuple(v / norm for v in q)
            struct.pack_into(fmt, binary, offset, *q)
            values.append(q)
        for key, reduce in [('min', min), ('max', max)]:
            if key in accessor:
                accessor[key] = [reduce(q[axis] for q in values) for axis in range(4)]
    chunks[bin_index] = (BIN_CHUNK, bytes(binary))
    return write_glb(doc, chunks)


def eye_occlusion(data):
    doc, chunks = read_glb(data)
    binary = next(data for kind, data in chunks if kind == BIN_CHUNK)
    primitives = [p for mesh in doc['meshes'] for p in mesh['primitives']]
    if len(primitives) != 1 or primitives[0].get('mode', 4) != 4:
        raise ValueError('Expected the LAM-20K triangle mesh')
    primitive = primitives[0]
    positions = read_accessor(doc, binary, primitive['attributes']['POSITION'])
    indices = [item[0] for item in read_accessor(doc, binary, primitive['indices'])]
    parents = list(range(len(positions)))

    def find(index):
        while parents[index] != index:
            parents[index] = parents[parents[index]]
            index = parents[index]
        return index

    if len(indices) % 3:
        raise ValueError('Incomplete triangles')
    for offset in range(0, len(indices), 3):
        a, b, c = indices[offset:offset + 3]
        parents[find(b)] = find(a)
        parents[find(c)] = find(a)
    components = {}
    for index in range(len(positions)):
        components.setdefault(find(index), []).append(index)
    ordered = sorted(components.values(), key=len, reverse=True)
    if len(ordered) != 3 or len(ordered[0]) <= max(map(len, ordered[1:])):
        raise ValueError('Expected a face and two separate eyeball components')
    eyes = sorted(ordered[1:], key=lambda eye: sum(positions[i][0] for i in eye) / len(eye), reverse=True)

    def ranges(vertices):
        result = []
        for index in vertices:
            if result and result[-1][1] + 1 == index:
                result[-1][1] = index
            else:
                result.append([index, index])
        return result

    # LAM's lEye lies at positive local X. Indices are canonical mesh/splat IDs.
    return {'left_iris': ranges(eyes[0]), 'right_iris': ranges(eyes[1])}


def prepare_avatar(source_path, output_path):
    source_path, output_path = Path(source_path), Path(output_path)
    if source_path.resolve() == output_path.resolve():
        raise ValueError('Output must differ from the source archive')
    with zipfile.ZipFile(source_path) as source:
        members = [(info, source.read(info.filename)) for info in source.infolist()]
    content = {info.filename: data for info, data in members}
    skins = [name for name in content if name.endswith('/skin.glb') or name == 'skin.glb']
    if len(skins) != 1:
        raise ValueError('Expected exactly one LAM skin.glb')
    prefix = skins[0][:-len('skin.glb')]
    marker_name, iris_name = prefix + CALIBRATION, prefix + 'iris_occlusion.json'
    marker = json.loads(content.get(marker_name, b'{}'))
    delta = PITCH_DOWN_DEGREES - float(marker.get('head_pitch_down_degrees', 0))
    if not math.isfinite(delta):
        raise ValueError('Invalid calibration angle')
    if abs(delta) > 1e-9:
        content[prefix + 'animation.glb'] = adjust_animation(content[prefix + 'animation.glb'], delta)
    if iris_name not in content:
        content[iris_name] = json.dumps(eye_occlusion(content[skins[0]]), separators=(',', ':')).encode()
    marker.update(version=1, head_pitch_down_degrees=PITCH_DOWN_DEGREES)
    content[marker_name] = json.dumps(marker, sort_keys=True, separators=(',', ':')).encode()
    output_path.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(output_path, 'w', zipfile.ZIP_DEFLATED) as output:
        for info, _ in members:
            output.writestr(info, content.pop(info.filename))
        for name, data in content.items():
            info = zipfile.ZipInfo(name)  # Stable metadata makes repeated preparation byte-identical.
            info.compress_type = zipfile.ZIP_DEFLATED
            output.writestr(info, data)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('source', type=Path)
    parser.add_argument('output', type=Path)
    args = parser.parse_args()
    prepare_avatar(args.source, args.output)
    print(args.output)


if __name__ == '__main__':
    main()
