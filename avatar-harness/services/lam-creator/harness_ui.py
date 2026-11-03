"""Attach automatic Harness saving to the existing LAM Gradio generator."""
import html
from contextlib import nullcontext
from functools import wraps
import json
import os
from pathlib import Path
import sys
from urllib.parse import urlencode
from urllib.request import ProxyHandler, Request, build_opener


def generation_handler(fn, gr, default_motion):
    @wraps(fn)
    def generate(image_path, video_params, working_dir, enable_oac_file):
        if not image_path:
            raise gr.Error('请先上传一张清晰的正面人像照片。')
        try:
            return fn(image_path, video_params or str(default_motion), working_dir, True)
        except gr.Error:
            raise
        except Exception as error:
            print('LAM generation failed:', error, file=sys.stderr, flush=True)
            messages = {
                'flametracking preprocess failed!': '照片预处理失败，未能检测到可用的人脸。请换用清晰、无遮挡的正面人像，脸部不要过小。',
                'flametracking optimize failed!': '人脸拟合失败，请换一张角度更正、光线均匀的照片后重试。',
                'flametracking export failed!': '人脸数据导出失败，请重试。',
            }
            raise gr.Error(messages.get(str(error), '形象生成失败，请稍后重试。')) from error
    return generate


def install(gr):
    from gradio.context import Context
    launch = gr.Blocks.launch

    def linked_launch(demo, *args, **kwargs):
        functions = list(demo.fns.values()) if isinstance(demo.fns, dict) else demo.fns
        index, generate = next((i, fn) for i, fn in enumerate(functions)
                               if getattr(fn.fn, '__name__', '') == 'core_fn')
        root = Path(os.environ.get('AVATAR_SERVICE_ROOT', '/data2/huangmr/code/avatar-harness'))
        motion = root / 'upstream/LAM/assets/sample_motion/export/D_ANgelo_Dinero/D_ANgelo_Dinero.mp4'
        if not motion.is_file():
            raise RuntimeError('LAM 默认预览动作文件缺失：' + str(motion))
        video = generate.inputs[1]
        video.value = video.postprocess(str(motion))
        video.label = '预览动作（默认即可）'
        generate.fn = generation_handler(generate.fn, gr, motion)
        archive = generate.outputs[-1]
        # A chatting avatar always needs the exported rig, not only a preview.
        export = generate.inputs[-1]
        export.value, export.interactive = True, False
        export.label = '生成后自动加入角色列表'

        def save_avatar(file, name):
            try:
                if file is None:
                    raise ValueError('LAM did not return an avatar ZIP')
                path = Path(file if isinstance(file, str) else file.name)
                url = os.environ.get('AVATAR_HARNESS_IMPORT_URL',
                                     'http://127.0.0.1:8765/api/characters/import')
                request = Request(url + '?' + urlencode({'name': name or ''}),
                                  data=path.read_bytes(),
                                  headers={'Content-Type': 'application/zip'}, method='POST')
                # Internal callbacks must not inherit the server's cloud proxy.
                with build_opener(ProxyHandler({})).open(request, timeout=30) as response:
                    character = json.load(response)
                return ('**已加入角色列表：' + html.escape(character['name'])
                        + '**　[返回数字人工作台](/)', gr.update(visible=False))
            except Exception as error:
                print('Harness avatar save failed:', error, file=sys.stderr, flush=True)
                return ('形象已生成，暂时未能加入角色列表。ZIP 已保留，请重试保存。',
                        gr.update(visible=True))

        # LAM calls launch inside its existing Blocks context. Re-entering that
        # same context makes Gradio 3.x append the entire layout to itself.
        with (nullcontext() if Context.root_block is demo else demo):
            name = gr.Textbox(label='形象名称', placeholder='可选，不填则自动命名', max_lines=1)
            status = gr.Markdown('生成完成后会自动加入数字人工作台的角色列表。')
            retry = gr.Button('重试保存', visible=False)
            demo.set_event_trigger('success', save_avatar, inputs=[archive, name],
                                   outputs=[status, retry], no_target=True, queue=False,
                                   trigger_after=index, trigger_only_on_success=True)
            retry.click(save_avatar, inputs=[archive, name], outputs=[status, retry], queue=False)
        kwargs['root_path'] = '/create-avatar'
        kwargs['show_api'] = False
        kwargs['show_error'] = True
        return launch(demo, *args, **kwargs)

    gr.Blocks.launch = linked_launch
