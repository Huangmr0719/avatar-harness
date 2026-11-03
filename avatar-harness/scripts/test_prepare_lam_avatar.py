"""Offline regressions for per-character LAM calibration (stdlib only)."""
import importlib.util
import json
import math
import struct
import tempfile
import unittest
import zipfile
from pathlib import Path

spec = importlib.util.spec_from_file_location('prepare_lam', Path(__file__).with_name('prepare-lam-avatar.py'))
prepare = importlib.util.module_from_spec(spec)
spec.loader.exec_module(prepare)


def glb(doc, binary):
    return prepare.write_glb(doc, [(prepare.JSON_CHUNK, b''), (prepare.BIN_CHUNK, binary)])


def skin():
    # Separate face and eyes, deliberately interleaved eye vertex IDs.
    positions = [(0, 0, 0), (0, 1, 0), (0, 1, 1), (0, 0, 1),
                 (1, 0, 0), (-1, 0, 0), (1, 1, 0), (-1, 1, 0), (1, 0, 1), (-1, 0, 1)]
    binary = b''.join(struct.pack('<fff', *p) for p in positions)
    indices = [0, 1, 2, 0, 2, 3, 4, 6, 8, 5, 7, 9]
    binary += struct.pack('<' + 'H' * len(indices), *indices)
    return glb({'meshes': [{'primitives': [{'attributes': {'POSITION': 0}, 'indices': 1}]}],
                'buffers': [{'byteLength': len(binary)}],
                'bufferViews': [{'buffer': 0, 'byteLength': 120}, {'buffer': 0, 'byteOffset': 120, 'byteLength': 24}],
                'accessors': [{'bufferView': 0, 'componentType': 5126, 'count': 10, 'type': 'VEC3'},
                              {'bufferView': 1, 'componentType': 5123, 'count': 12, 'type': 'SCALAR'}]}, binary)


def animation():
    binary = struct.pack('<ff', 0, 1) + struct.pack('<ffffffff', 0, 0, 0, 1, 0, 0, 0, 1)
    return glb({'nodes': [{'name': 'head'}],
                'buffers': [{'byteLength': len(binary)}],
                'bufferViews': [{'buffer': 0, 'byteLength': 8}, {'buffer': 0, 'byteOffset': 8, 'byteLength': 32}],
                'accessors': [{'bufferView': 0, 'componentType': 5126, 'count': 2, 'type': 'SCALAR'},
                              {'bufferView': 1, 'componentType': 5126, 'count': 2, 'type': 'VEC4'}],
                'animations': [{'name': name, 'samplers': [{'input': 0, 'output': 1}],
                                'channels': [{'sampler': 0, 'target': {'node': 0, 'path': 'rotation'}}]}
                               for name in reversed(prepare.CLIPS)]}, binary)


class PrepareAvatarTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.source = Path(self.temp.name) / 'source.zip'
        self.output = Path(self.temp.name) / 'prepared.zip'
        self.members = {'avatar/skin.glb': skin(), 'avatar/animation.glb': animation(),
                        'avatar/offset.ply': b'original gaussian appearance', 'avatar/vertex_order.json': b'[2,0,1]'}

    def write_source(self):
        with zipfile.ZipFile(self.source, 'w', zipfile.ZIP_DEFLATED) as archive:
            for name, data in self.members.items():
                archive.writestr(name, data)

    def test_per_character_eyes_and_downward_rotation_preserve_source(self):
        self.write_source()
        source = self.source.read_bytes()
        prepare.prepare_avatar(self.source, self.output)
        self.assertEqual(self.source.read_bytes(), source)
        with zipfile.ZipFile(self.output) as archive:
            for name, data in self.members.items():
                if not name.endswith('animation.glb'):
                    self.assertEqual(archive.read(name), data)
            self.assertEqual(json.loads(archive.read('avatar/iris_occlusion.json')),
                             {'left_iris': [[4, 4], [6, 6], [8, 8]], 'right_iris': [[5, 5], [7, 7], [9, 9]]})
            doc, chunks = prepare.read_glb(archive.read('avatar/animation.glb'))
            self.assertEqual([clip['name'] for clip in doc['animations']], prepare.CLIPS)
            binary = next(data for kind, data in chunks if kind == prepare.BIN_CHUNK)
            for x, y, z, w in prepare.read_accessor(doc, binary, 1):
                # Shared clip keys must be shifted once, and +X points gaze downward.
                self.assertAlmostEqual(math.degrees(2 * math.atan2(x, w)), 6, places=5)
                self.assertLess(-2 * x * w, 0)
                self.assertEqual((y, z), (0, 0))

    def test_repeated_preparation_is_byte_identical(self):
        self.write_source()
        prepare.prepare_avatar(self.source, self.output)
        repeated = Path(self.temp.name) / 'repeated.zip'
        prepare.prepare_avatar(self.output, repeated)
        self.assertEqual(repeated.read_bytes(), self.output.read_bytes())

    def test_existing_occlusion_is_preserved(self):
        occlusion = b'{"left_iris":[[4,8]],"right_iris":[[5,9]],"left_iris_north":[[4,4]]}'
        self.members['avatar/iris_occlusion.json'] = occlusion
        self.write_source()
        prepare.prepare_avatar(self.source, self.output)
        with zipfile.ZipFile(self.output) as archive:
            self.assertEqual(archive.read('avatar/iris_occlusion.json'), occlusion)

    def test_calibration_upgrade_applies_only_remaining_angle(self):
        # An earlier 2-degree calibration needs only another 4 degrees.
        self.members['avatar/animation.glb'] = prepare.adjust_animation(animation(), 2)
        self.members['avatar/harness_calibration.json'] = b'{"head_pitch_down_degrees":2}'
        self.write_source()
        prepare.prepare_avatar(self.source, self.output)
        with zipfile.ZipFile(self.output) as archive:
            doc, chunks = prepare.read_glb(archive.read('avatar/animation.glb'))
            binary = next(data for kind, data in chunks if kind == prepare.BIN_CHUNK)
            x, _, _, w = prepare.read_accessor(doc, binary, 1)[0]
            self.assertAlmostEqual(math.degrees(2 * math.atan2(x, w)), 6, places=5)

    def test_same_output_rejected_without_touching_original(self):
        self.write_source()
        source = self.source.read_bytes()
        with self.assertRaisesRegex(ValueError, 'differ'):
            prepare.prepare_avatar(self.source, self.source)
        self.assertEqual(self.source.read_bytes(), source)


if __name__ == '__main__':
    unittest.main()
