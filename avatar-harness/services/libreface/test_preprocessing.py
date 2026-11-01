"""Regression checks run in data2's existing au_gen environment, no weights needed."""
import io
import unittest
from types import SimpleNamespace
from unittest.mock import patch

import numpy as np
from PIL import Image
import app

class CaptureComplete(Exception):
    pass

class PreprocessingTest(unittest.TestCase):
    def test_analyze_keeps_official_eye_block_order_and_alignment_resolution(self):
        landmarks = [SimpleNamespace(x=.5, y=.7) for _ in range(468)]
        # MediaPipe's anatomical left eye appears on the right of the image.
        left = {i for pair in app.mp.solutions.face_mesh.FACEMESH_LEFT_EYE for i in pair}
        right = {i for pair in app.mp.solutions.face_mesh.FACEMESH_RIGHT_EYE for i in pair}
        for i in left: landmarks[i] = SimpleNamespace(x=.65,y=.35)
        for i in right: landmarks[i] = SimpleNamespace(x=.35,y=.35)
        engine = app.Engine.__new__(app.Engine)
        engine.fer = SimpleNamespace(model_name="test-metadata")
        engine.mesh = SimpleNamespace(process=lambda image: SimpleNamespace(multi_face_landmarks=[SimpleNamespace(landmark=landmarks)]))
        stream = io.BytesIO(); Image.new('RGB',(64,64)).save(stream,format='JPEG')
        def capture(image, points, **kwargs):
            self.assertGreater(np.mean(np.asarray(points)[:16,0]),np.mean(np.asarray(points)[16:32,0]),
                'Reversed eye blocks alter crop rotation and size before AU and FER inference')
            self.assertEqual(kwargs.get('transform_size',512),512,'Use the upstream alignment resolution')
            raise CaptureComplete
        with patch.object(app,'image_align',side_effect=capture), self.assertRaises(CaptureComplete):
            engine.analyze(stream.getvalue())

if __name__ == '__main__': unittest.main()
