"""Official LAM-A2E CUDA provider. Serve on loopback through an SSH tunnel."""
from __future__ import annotations

import asyncio
import base64
import copy
from contextlib import asynccontextmanager
import os
from pathlib import Path
import runpy
import sys
import threading
import time

import numpy as np
import torch
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field

ROOT = Path(os.environ.get("AVATAR_SERVICE_ROOT", "/data2/huangmr/code/avatar-harness"))
REPO = ROOT / "upstream/LAM_Audio2Expression"
WINDOW_SAMPLES = 16000 * 64 // 30


class Driver:
    def __init__(self):
        if not torch.cuda.is_available():
            raise RuntimeError("Working CUDA GPU required; CPU fallback is disabled")
        sys.path.insert(0, str(REPO))
        from models import build_model
        from models.utils import ARKitBlendShape

        torch.set_num_threads(2)
        cfg = runpy.run_path(str(REPO / "configs/lam_audio2exp_config_streaming.py"))
        model_cfg = copy.deepcopy(cfg["model"])
        model_cfg["backbone"]["wav2vec2_config_path"] = str(REPO / "configs/wav2vec2_config.json")
        model_cfg["backbone"]["pretrained_encoder_path"] = ""
        self.model = build_model(model_cfg)
        checkpoint = torch.load(REPO / "pretrained_models/lam_audio2exp_streaming.tar", map_location="cpu", weights_only=True)
        state = {k.removeprefix("module."): v for k, v in checkpoint["state_dict"].items()}
        self.model.load_state_dict(state, strict=True)
        self.model.eval().cuda()
        self.names = list(ARKitBlendShape)
        self.lock = threading.Lock()
        self.calls = 0
        self.last_ms = 0.0
        self.predict(np.zeros(WINDOW_SAMPLES, dtype=np.float32), 0)

    def health(self):
        return {"status": "ready", "backend": "cuda", "model": "official-lam-a2e-streaming",
                "gpu": torch.cuda.get_device_name(0), "names": self.names, "sample_rate": 16000,
                "input_samples": WINDOW_SAMPLES, "identity_classes": 12,
                "calls": self.calls, "last_inference_ms": self.last_ms}

    @torch.inference_mode()
    def predict(self, audio: np.ndarray, identity: int):
        queued = time.perf_counter()
        with self.lock:
            start = time.perf_counter()
            x = torch.from_numpy(audio.copy()).unsqueeze(0).cuda()
            style = torch.nn.functional.one_hot(torch.tensor([identity], device="cuda"), 12)
            result = self.model({"input_audio_array": x, "id_idx": style, "time_steps": 64})["pred_exp"]
            values = result.squeeze(0).float().cpu().numpy()
            if values.shape != (64, 52) or not np.isfinite(values).all():
                raise RuntimeError("LAM-A2E returned invalid facial coefficients")
            self.last_ms = (time.perf_counter() - start) * 1000
            self.calls += 1
            return {"weights": np.clip(values, 0, 1).reshape(-1).tolist(), "frames": 64,
                    "elapsed_ms": self.last_ms, "queue_ms": (start - queued) * 1000, "backend": "cuda"}


class InvokeRequest(BaseModel):
    capability: str = "avatar"
    input: dict = Field(default_factory=dict)
    context: dict = Field(default_factory=dict)


@asynccontextmanager
async def lifespan(app: FastAPI):
    app.state.driver = await asyncio.to_thread(Driver)
    app.state.capacity = asyncio.Semaphore(4)
    yield


app = FastAPI(title="LAM-A2E GPU Provider", lifespan=lifespan)


@app.get("/health")
def health():
    return app.state.driver.health()


@app.post("/invoke")
async def invoke(request: InvokeRequest):
    if request.capability != "avatar":
        raise HTTPException(422, "Only the avatar capability is supported")
    operation = request.input.get("operation")
    if operation == "health":
        output = app.state.driver.health()
    elif operation == "describe":
        output = {"renderer": {"kind": "lam", "asset_url": "/models/lam/avatar.zip", "driver": "remote"}}
    elif operation == "infer_window":
        if request.input.get("sample_rate") != 16000 or request.input.get("format") != "pcm_f32le":
            raise HTTPException(422, "Expected 16 kHz mono pcm_f32le")
        encoded = request.input.get("audio_base64", "")
        if not isinstance(encoded, str) or len(encoded) > 190000:
            raise HTTPException(422, "Audio window is too large")
        try:
            raw = base64.b64decode(encoded, validate=True)
        except (ValueError, TypeError):
            raise HTTPException(422, "Invalid audio encoding")
        if len(raw) != WINDOW_SAMPLES * 4:
            raise HTTPException(422, f"Expected {WINDOW_SAMPLES} float32 samples")
        samples = np.frombuffer(raw, dtype="<f4")
        if not np.isfinite(samples).all() or np.max(np.abs(samples)) > 1.001:
            raise HTTPException(422, "Audio samples must be finite and normalized")
        identity = request.input.get("identity", 0)
        if type(identity) is not int or not 0 <= identity < 12:
            raise HTTPException(422, "Identity must be an integer from 0 to 11")
        async with app.state.capacity:
            output = await asyncio.to_thread(app.state.driver.predict, samples, identity)
    else:
        raise HTTPException(422, "Unknown operation")
    return {"status": "ok", "output": output, "error": ""}
