"""EmotiEffNet ONNX adapter; preprocessing/order follow upstream EmotiEffLib.
Upstream: sb-ai-lab/EmotiEffLib, Apache-2.0, pinned in sources.lock.json.
"""
from pathlib import Path
import sys
import os
import cv2
import numpy as np
import onnxruntime as ort

LABELS = ('angry','contempt','disgusted','fearful','happy','neutral','sad','surprised')
MODEL = os.environ.get('EMOTION_MODEL','enet_b2_8')
if MODEL not in ('enet_b2_8','enet_b0_8_best_vgaf'): raise ValueError('Unsupported emotion model')


def preprocess(rgb,size=224):
    # Keep RGB (do not convert to BGR). Upstream ONNX uses OpenCV linear resize.
    x = cv2.resize(rgb,(size,size)) / 255
    for i,(mean,std) in enumerate(zip([.485,.456,.406],[.229,.224,.225])):
        x[...,i] = (x[...,i] - mean) / std
    return x.transpose(2,0,1).astype(np.float32)[None]


def decode(logits,model_name=MODEL):
    values = np.asarray(logits,dtype=np.float64).reshape(-1)
    if values.shape != (8,) or not np.isfinite(values).all():
        raise ValueError('Invalid EmotiEffNet output')
    probability = np.exp(values - values.max()); probability /= probability.sum()
    index = int(probability.argmax())
    return {'label':LABELS[index], 'score':round(float(probability[index]),4),
            'score_type':'softmax_uncalibrated', 'model':model_name,
            'scores':{name:round(float(p),4) for name,p in zip(LABELS,probability)}}


class EmotiEffNet:
    def __init__(self,path,detector_device='cpu'):
        sys.path.insert(0,str(Path(__file__).parent / 'python-packages'))
        from facenet_pytorch import MTCNN
        import torch
        self.detector_device = str(detector_device)
        self.detector = MTCNN(keep_all=True,post_process=False,min_face_size=40,device=detector_device)
        # Load all three detector stages before declaring the service ready.
        # The first CUDA kernel otherwise consumes the camera request deadline.
        if self.detector_device.startswith('cuda'):
            with torch.inference_mode():
                for net,size in ((self.detector.pnet,40),(self.detector.rnet,24),(self.detector.onet,48)):
                    net(torch.zeros((1,3,size,size),device=detector_device))
            torch.cuda.synchronize(detector_device)
        options = ort.SessionOptions(); options.intra_op_num_threads=2; options.inter_op_num_threads=1
        self.session = ort.InferenceSession(str(Path(path)),sess_options=options,providers=['CPUExecutionProvider'])
        self.input = self.session.get_inputs()[0].name
        self.size = int(self.session.get_inputs()[0].shape[-1])
        self.model_name = Path(path).stem
        if self.size not in (224,260): raise ValueError("Unexpected EmotiEffNet image size")
        self.session.run(None,{self.input:np.zeros((1,3,self.size,self.size),np.float32)})

    def predict(self,rgb):
        return decode(self.session.run(None,{self.input:preprocess(rgb,self.size)})[0],self.model_name)


    def predict_image(self,rgb):
        boxes,confidence=self.detector.detect(rgb)
        if boxes is None: return None
        boxes=boxes[np.asarray(confidence)>.9]
        if len(boxes)!=1: return None
        return self.predict(crop_box(rgb,boxes[0]))


def crop_box(rgb,box):
    h,w=rgb.shape[:2]
    x1,y1,x2,y2=[int(x) for x in box]
    x1=max(0,x1);y1=max(0,y1);x2=min(w,x2);y2=min(h,y2)
    if x2<=x1 or y2<=y1: raise ValueError('Empty face crop')
    return rgb[y1:y2,x1:x2]
