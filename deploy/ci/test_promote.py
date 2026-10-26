import copy
import importlib.util
import json
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('promote', Path(__file__).with_name('promote.py'))
promote = importlib.util.module_from_spec(spec)
spec.loader.exec_module(promote)

class PromotionTests(unittest.TestCase):
    def setUp(self):
        self.manifest = json.loads((Path(__file__).parents[1] / 'k3s/workloads.json').read_text())
        self.images = {'backend': 'ghcr.io/traliaa/trade-bot-backend@sha256:' + 'a' * 64,
                       'frontend': 'ghcr.io/traliaa/trade-bot-frontend@sha256:' + 'b' * 64}

    def test_changes_only_images_and_pull_policy(self):
        original = copy.deepcopy(self.manifest)
        result = promote.candidate(self.manifest, self.images)
        expected = copy.deepcopy(original)
        for item in expected['items']:
            if item['kind'] not in ('Deployment', 'StatefulSet'):
                continue
            role = 'backend' if item['kind'] == 'StatefulSet' else 'frontend'
            c = item['spec']['template']['spec']['containers'][0]
            c['image'] = self.images[role]
            c['imagePullPolicy'] = 'IfNotPresent'
        self.assertEqual(result, expected)
        self.assertEqual(self.manifest, original)

    def test_rejects_unpinned_foreign_or_wrong_role_images(self):
        for image in ['ghcr.io/traliaa/trade-bot-backend:latest',
                      'evil.test/trade-bot-backend@sha256:' + 'a' * 64,
                      self.images['frontend'],
                      'ghcr.io/traliaa/trade-bot-backend@sha256:' + 'a' * 63]:
            with self.subTest(image=image), self.assertRaises(ValueError):
                promote.candidate(self.manifest, dict(self.images, backend=image))

    def test_rejects_missing_duplicate_or_relocated_workloads(self):
        for mutation in ('missing', 'duplicate', 'namespace', 'node', 'replicas', 'strategy'):
            m = copy.deepcopy(self.manifest)
            backend = next(x for x in m['items'] if x['kind'] == 'StatefulSet')
            if mutation == 'missing': m['items'].remove(backend)
            elif mutation == 'duplicate': m['items'].append(copy.deepcopy(backend))
            elif mutation == 'namespace': backend['metadata']['namespace'] = 'default'
            elif mutation == 'node': backend['spec']['template']['spec']['nodeSelector'] = {}
            elif mutation == 'replicas': backend['spec']['replicas'] = 2
            else: backend['spec']['updateStrategy']['type'] = 'RollingUpdate'
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                promote.candidate(m, self.images)

if __name__ == '__main__': unittest.main()
