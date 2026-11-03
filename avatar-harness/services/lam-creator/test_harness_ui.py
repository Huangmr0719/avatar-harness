import unittest
from types import SimpleNamespace

from harness_ui import generation_handler


class CreatorInputTests(unittest.TestCase):
    class UserError(Exception):
        pass

    def setUp(self):
        self.gr = SimpleNamespace(Error=self.UserError)

    def test_missing_motion_uses_preset_and_keeps_working_state(self):
        seen = []
        state = object()
        def core_fn(*args):
            seen.append(args)
            return ('image', 'video', 'avatar.zip')
        generate = generation_handler(core_fn, self.gr, '/preset/default.mp4')
        self.assertEqual(generate('/photo.png', None, state, False), ('image', 'video', 'avatar.zip'))
        self.assertEqual(seen, [('/photo.png', '/preset/default.mp4', state, True)])

    def test_selected_motion_is_preserved(self):
        generate = generation_handler(lambda *args: args, self.gr, '/preset/default.mp4')
        self.assertEqual(generate('/photo.png', '/preset/chosen.mp4', None, True)[1], '/preset/chosen.mp4')

    def test_missing_photo_does_not_start_model(self):
        def core_fn(*args):
            self.fail('model must not run without a photo')
        generate = generation_handler(core_fn, self.gr, '/preset/default.mp4')
        with self.assertRaisesRegex(self.UserError, '请先上传'):
            generate(None, None, None, True)

    def test_preprocessing_failure_gives_actionable_error(self):
        def core_fn(*args):
            raise AssertionError('flametracking preprocess failed!')
        generate = generation_handler(core_fn, self.gr, '/preset/default.mp4')
        with self.assertRaisesRegex(self.UserError, '人脸'):
            generate('/photo.png', None, None, True)


if __name__ == '__main__':
    unittest.main()
