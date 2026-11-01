"""Resident LibreFace adapter. Loopback only; camera frames stay in memory."""
import base64
import hashlib
import io
import os
import sys
import threading
import time
from contextlib import asynccontextmanager
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

import cv2
import mediapipe as mp
import numpy as np
import torch
from fastapi import FastAPI, HTTPException
from PIL import Image
from pydantic import BaseModel

ROOT = Path(os.environ.get('LIBREFACE_ROOT', '/data2/huangmr/code/LibreFace'))
sys.path.insert(0, str(ROOT))
from libreface.AU_Recognition.solver_inference_image import solver_inference_image
from emotion import EmotiEffNet, MODEL as EMOTION_MODEL
from libreface.detect_mediapipe_image import image_align

lock = threading.Lock()
engine = None
AU_MODEL = 'libreface_2_0_repvgg_fm2026'
AU_WEIGHTS_SHA256 = '6b50fa3421de494fa6c4325477716b652878a27242ab37976d492e4c9fa0d8f5'
# Match LibreFace's get_aligned_image() landmark block and within-block order.
# image_align's historical variable names are misleading: its FIRST block must
# be MediaPipe LEFT_EYE. Reversing the blocks alters rotation and crop scale.
ALIGNMENT_GROUPS = (
    (263,249,390,373,374,380,381,382,362,466,388,387,386,385,384,398),
    (33,7,163,144,145,153,154,155,133,246,161,160,159,158,157,173),
    (61,146,91,181,84,17,314,405,321,375,291,185,40,39,37,0,267,269,270,409,
     78,95,88,178,87,14,317,402,318,324,308,191,80,81,82,13,312,311,310,415),
)

class Engine:
    def __init__(self):
        torch.set_num_threads(2)
        self.device = 'cuda:0' if torch.cuda.is_available() else 'cpu'
        weights = ROOT / 'libreface_weights/AU_Recognition/weights/repvgg_fm2026.pt'
        digest = hashlib.sha256(weights.read_bytes()).hexdigest()
        if digest != AU_WEIGHTS_SHA256:
            raise RuntimeError('LibreFace 2.0 AU checkpoint checksum mismatch')
        opts = SimpleNamespace(device=self.device, image_size=256, crop_size=224,
            num_labels=12, fm_distillation=False, half_precision=False,
            dropout=.1, model_name='repvgg', ckpt_path=str(weights), weights_download_id='')
        # Load the full checkpoint strictly; a fresh ImageNet download is not
        # needed and must not change the shared data2 environment.
        import timm
        original = timm.create_model
        def create_model(*args, **kwargs):
            kwargs['pretrained'] = False
            return original(*args, **kwargs)
        with patch('timm.create_model', side_effect=create_model):
            self.au = solver_inference_image(opts)
        self.au.load_best_ckpt()
        self.au.eval()
        fer_path = Path(os.environ.get('EMOTIEFF_WEIGHTS', str(Path(__file__).parent / 'weights' / (EMOTION_MODEL+'.onnx'))))
        self.fer = EmotiEffNet(fer_path, detector_device=self.device)
        self.mesh = mp.solutions.face_mesh.FaceMesh(static_image_mode=True, max_num_faces=2,
            refine_landmarks=True, min_detection_confidence=.5)
        self.transform = self.au.image_transform
        with torch.inference_mode():
            x = torch.zeros(3,224,224,device=self.device)
            self.au.image_inference(x)

    def analyze(self, raw):
        start = time.perf_counter()
        try:
            with Image.open(io.BytesIO(raw)) as source:
                if source.width > 1920 or source.height > 1080: raise ValueError('image dimensions')
                image = source.convert('RGB')
        except Exception:
            raise HTTPException(422, 'Invalid camera image')
        result = self.mesh.process(np.asarray(image))
        faces = result.multi_face_landmarks or []
        output = {'source':'emotieffnet+libreface_au', 'au_source':AU_MODEL, 'au_weights_sha256':AU_WEIGHTS_SHA256, 'expression_source':self.fer.model_name, 'face_count':len(faces), 'face_detected':len(faces)==1,
                  'au_intensity_scale':[0,5], 'expression':None}
        if len(faces) != 1:
            output['quality'] = 'no_face' if not faces else 'multiple_faces'
            return output
        landmarks = faces[0].landmark
        points = [[landmarks[i].x,landmarks[i].y] for group in ALIGNMENT_GROUPS for i in group]
        aligned = image_align(image, points)
        x = self.transform(aligned)
        with torch.inference_mode():
            # Official inference applies output * 5 and clamps to [0,5]. These
            # are regression intensities, not AU-presence probabilities.
            intensity = self.au.image_inference(x)[0].cpu().tolist()
            output['au_intensities'] = {f'AU{k:02}':round(v,3) for k,v in zip(self.au.aus,intensity)}
        fer_started = time.perf_counter()
        output['expression'] = self.fer.predict_image(np.asarray(image))
        output['expression_ms'] = round((time.perf_counter()-fer_started)*1000,1)
        output.update(quality='single_face', inference_ms=round((time.perf_counter()-start)*1000,1))
        return output

@asynccontextmanager
async def lifespan(app):
    global engine
    engine = Engine()
    yield
    engine.mesh.close()

app = FastAPI(lifespan=lifespan)
class Request(BaseModel):
    input: dict = {}

@app.get('/health')
def health():
    return {'status':'ready' if engine else 'loading', 'model':'EmotiEffNet + LibreFace 2.0 AU', 'au_model':AU_MODEL, 'au_weights_sha256':AU_WEIGHTS_SHA256,
            'device':engine.device if engine else None, 'expression_available':bool(engine and engine.fer), 'expression_model':engine.fer.model_name if engine else EMOTION_MODEL, 'expression_backend':'onnx_cpu',
            'face_detector_device':engine.fer.detector_device if engine else None,
            'preprocessing':'libreface_reference_v2'}

@app.post('/invoke')
def invoke(request: Request):
    if request.input.get('operation') == 'health': return {'status':'ok','output':health()}
    encoded = request.input.get('image_base64','')
    if not isinstance(encoded,str) or len(encoded)>500000: raise HTTPException(413,'Frame too large')
    try: raw = base64.b64decode(encoded,validate=True)
    except Exception: raise HTTPException(422,'Invalid image encoding')
    if not lock.acquire(blocking=False): raise HTTPException(429,'Analysis busy')
    try: return {'status':'ok','output':engine.analyze(raw)}
    finally: lock.release()
