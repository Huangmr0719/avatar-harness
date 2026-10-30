"""Run the official LAM UI, rejecting missing learned checkpoint parameters."""
import os
from pathlib import Path
import sys

root = Path(os.environ.get('AVATAR_SERVICE_ROOT', '/data2/huangmr/code/avatar-harness'))
sys.path.insert(0, str(root / 'upstream/LAM'))
import app_lam


def build_model(cfg):
    from lam.models import ModelLAM
    from safetensors.torch import load_file
    model = ModelLAM(**cfg.model)
    state = load_file(str(Path(cfg.model_name) / 'model.safetensors'), device='cpu')
    incompatible = model.load_state_dict(state, strict=False)
    learned = set(dict(model.named_parameters()))
    missing = learned.intersection(incompatible.missing_keys)
    if missing:
        raise RuntimeError('Checkpoint missing learned parameters: ' + ', '.join(sorted(missing)))
    print('LAM learned parameters loaded; missing buffers:', incompatible.missing_keys, flush=True)
    return model


app_lam._build_model = build_model
app_lam.launch_gradio_app()
