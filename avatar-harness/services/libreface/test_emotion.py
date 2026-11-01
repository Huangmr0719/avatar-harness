import unittest
import numpy as np
from emotion import preprocess,decode,crop_box,LABELS,EmotiEffNet
from types import SimpleNamespace

class EmotionTest(unittest.TestCase):
    def test_rgb_normalization_layout_and_onnx_label_order(self):
        red=np.zeros((50,80,3),np.uint8);red[:,:,0]=255
        x=preprocess(red)
        self.assertEqual(x.shape,(1,3,224,224));self.assertEqual(x.dtype,np.float32)
        np.testing.assert_allclose(x[0,:,100,100],[(1-.485)/.229,-.456/.224,-.406/.225],atol=1e-6)
        for index,label in enumerate(LABELS):
            logits=np.full(8,-10.);logits[index]=10
            result=decode(logits);self.assertEqual(result['label'],label)
            self.assertAlmostEqual(sum(result['scores'].values()),1,places=3)
        with self.assertRaises(ValueError):decode(np.zeros(12))
        with self.assertRaises(ValueError):decode(np.full(8,np.nan))
    def test_face_rectangle_is_clipped_without_wrapping_negative_indices(self):
        rgb=np.zeros((40,50,3),np.uint8)
        crop=crop_box(rgb,[-5,-4,55,44])
        self.assertEqual(crop.shape,rgb.shape)

    def test_detector_abstains_when_no_single_face_is_confident(self):
        model=EmotiEffNet.__new__(EmotiEffNet)
        model.detector=SimpleNamespace(detect=lambda rgb:(None,None))
        self.assertIsNone(model.predict_image(np.zeros((40,50,3),np.uint8)))
        model.detector=SimpleNamespace(detect=lambda rgb:(np.zeros((2,4)),np.ones(2)))
        self.assertIsNone(model.predict_image(np.zeros((40,50,3),np.uint8)))

if __name__=='__main__' :unittest.main()
