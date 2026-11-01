"""Integration parity against upstream preprocessing; public fixtures, not an accuracy benchmark.
Run on data2: CUDA_VISIBLE_DEVICES=3 python -m unittest test_reference_parity -v
"""
import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import cv2
import numpy as np
import torch
from PIL import Image
import app
from libreface.detect_mediapipe_image import get_aligned_image
from emotion import LABELS, EmotiEffNet

@unittest.skipUnless((app.ROOT / 'examples/sample_disfa.png').is_file(), 'Requires existing LibreFace public fixtures and weights')
class ReferenceParityTest(unittest.TestCase):
    @unittest.skipUnless(torch.cuda.is_available(), 'Requires the existing CUDA service device')
    def test_gpu_face_detection_preserves_public_prediction(self):
        torch.set_num_threads(2)
        weights = Path(app.__file__).parent / 'weights/enet_b2_8.onnx'
        rgb = cv2.cvtColor(cv2.imread(str(app.ROOT / 'examples/sample_disfa.png')), cv2.COLOR_BGR2RGB)
        cpu = EmotiEffNet(weights)
        gpu = EmotiEffNet(weights, detector_device='cuda:0')
        cpu_boxes, cpu_scores = cpu.detector.detect(rgb)
        gpu_boxes, gpu_scores = gpu.detector.detect(rgb)
        np.testing.assert_allclose(np.asarray(gpu_boxes, dtype=float), np.asarray(cpu_boxes, dtype=float), atol=.1, rtol=0)
        np.testing.assert_allclose(np.asarray(gpu_scores, dtype=float), np.asarray(cpu_scores, dtype=float), atol=.0001, rtol=0)
        expected, actual = cpu.predict_image(rgb), gpu.predict_image(rgb)
        self.assertEqual(actual['label'], expected['label'])
        for label in LABELS:
            self.assertAlmostEqual(actual['scores'][label], expected['scores'][label], delta=.002)

    def test_public_image_and_video_frames_match_official_inputs_and_predictions(self):
        engine = app.Engine()
        inputs = [('sample_image',Image.open(app.ROOT / 'examples/sample_disfa.png').convert('RGB'))]
        video = cv2.VideoCapture(str(app.ROOT / 'examples/sample_disfa.avi'))
        count = int(video.get(cv2.CAP_PROP_FRAME_COUNT))
        for index in np.linspace(0,max(0,count-1),7,dtype=int):
            video.set(cv2.CAP_PROP_POS_FRAMES,int(index)); ok,frame=video.read()
            if ok: inputs.append((f'frame_{index}',Image.fromarray(cv2.cvtColor(frame,cv2.COLOR_BGR2RGB))))
        video.release()
        report = []
        try:
            with tempfile.TemporaryDirectory() as directory:
                for name,image in inputs:
                    with self.subTest(name=name):
                        path=Path(directory)/f'{name}.png'; image.save(path)
                        official_path,*_=get_aligned_image(str(path),temp_dir=directory)
                        with Image.open(official_path) as official: expected_crop=np.asarray(official).copy()
                        captures=[]; face_crops=[]; original=app.image_align; original_predict=engine.fer.predict
                        def capture_face(rgb):
                            face_crops.append(rgb.copy());return original_predict(rgb)
                        def capture(*args,**kwargs):
                            aligned=original(*args,**kwargs);captures.append(np.asarray(aligned).copy());return aligned
                        buffer=io.BytesIO();image.save(buffer,format='PNG')
                        with patch.object(app,'image_align',side_effect=capture), patch.object(engine.fer,'predict',side_effect=capture_face): actual=engine.analyze(buffer.getvalue())
                        self.assertTrue(actual['face_detected'])
                        self.assertEqual(captures[0].shape,expected_crop.shape)
                        crop_error=float(np.abs(captures[0].astype(float)-expected_crop.astype(float)).max())
                        self.assertEqual(crop_error,0,'Adapter crop must be pixel-identical to upstream')
                        x=engine.au.transform_image_inference(official_path)
                        expected_au=engine.au.image_inference(x)[0].cpu().tolist()
                        au_error=max(abs(actual['au_intensities'][f'AU{k:02}']-v) for k,v in zip(engine.au.aus,expected_au))
                        self.assertEqual(actual['au_source'],'libreface_2_0_repvgg_fm2026')
                        self.assertEqual(len(actual['au_intensities']),12)
                        self.assertTrue(all(0 <= v <= 5 for v in actual['au_intensities'].values()))
                        self.assertLessEqual(au_error,.00051)
                        # Independent transcription of the upstream ONNX preprocessor:
                        ref_tensor=cv2.resize(face_crops[0],(engine.fer.size,engine.fer.size))/255
                        for c,(mean,std) in enumerate(zip([.485,.456,.406],[.229,.224,.225])):
                            ref_tensor[...,c]=(ref_tensor[...,c]-mean)/std
                        ref_tensor=ref_tensor.transpose(2,0,1).astype('float32')[None]
                        logits=engine.fer.session.run(None,{engine.fer.input:ref_tensor})[0][0].astype(float)
                        scores=np.exp(logits-logits.max());scores/=scores.sum()
                        label=LABELS[int(scores.argmax())]
                        self.assertEqual(actual['expression']['label'],label)
                        score_error=max(abs(actual['expression']['scores'][k]-v) for k,v in zip(LABELS,scores))
                        self.assertLessEqual(score_error,.000051)
                        report.append({'input':name,'crop_max_pixel_error':crop_error,'au_max_error':round(au_error,7),'fer_max_error':round(score_error,7),'label':label})
        finally: engine.mesh.close()
        self.assertGreaterEqual(len(report),8)
        Path('/tmp/emotieff-reference-parity.json').write_text(json.dumps(report,indent=2))
        print(json.dumps({'reference_parity':report}))

if __name__ == '__main__': unittest.main()
