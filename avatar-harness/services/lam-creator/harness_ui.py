"""Attach automatic Harness saving to the existing LAM Gradio generator."""
import html
from contextlib import nullcontext
import json
import os
from pathlib import Path
import sys
from urllib.parse import urlencode
from urllib.request import ProxyHandler, Request, build_opener


def install(gr):
    from gradio.context import Context
    launch = gr.Blocks.launch

    def linked_launch(demo, *args, **kwargs):
        functions = list(demo.fns.values()) if isinstance(demo.fns, dict) else demo.fns
        index, generate = next((i, fn) for i, fn in enumerate(functions)
                               if getattr(fn.fn, '__name__', '') == 'core_fn')
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
        return launch(demo, *args, **kwargs)

    gr.Blocks.launch = linked_launch
