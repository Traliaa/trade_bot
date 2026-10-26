#!/usr/bin/env python3
"""Produce a reviewable image-only GitOps update; never deploy or touch secrets."""
import argparse
import copy
import json
from pathlib import Path
import re


def candidate(manifest, images):
    for role in ('backend', 'frontend'):
        if not re.fullmatch(r'ghcr\.io/traliaa/trade-bot-' + role + r'@sha256:[0-9a-f]{64}', images[role]):
            raise ValueError('Expected pinned project image for ' + role)
    result = copy.deepcopy(manifest)
    expected = {'StatefulSet': ('trade-bot', 'backend'), 'Deployment': ('trade-webapp', 'frontend')}
    seen = set()
    for item in result['items']:
        if item['kind'] not in expected:
            continue
        name, role = expected[item['kind']]
        if item['metadata']['name'] != name or item['metadata']['namespace'] != 'trade-bot' or role in seen:
            raise ValueError('Unexpected or duplicate workload')
        seen.add(role)
        if role == 'backend':
            spec = item['spec']
            if (spec['replicas'] != 1 or spec['updateStrategy']['type'] != 'OnDelete'
                or spec['template']['spec'].get('nodeSelector') != {'kubernetes.io/hostname': 'k3s-dacha'}):
                raise ValueError('Single-site backend constraints changed')
        containers = item['spec']['template']['spec']['containers']
        if len(containers) != 1 or containers[0]['name'] != name:
            raise ValueError('Unexpected container layout')
        containers[0]['image'] = images[role]
        containers[0]['imagePullPolicy'] = 'IfNotPresent'
    if seen != {'backend', 'frontend'}:
        raise ValueError('Both workloads are required')
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--manifest', type=Path, default=Path('deploy/k3s/workloads.json'))
    parser.add_argument('--backend-image', required=True)
    parser.add_argument('--frontend-image', required=True)
    args = parser.parse_args()
    manifest = json.loads(args.manifest.read_text())
    updated = candidate(manifest, {'backend': args.backend_image, 'frontend': args.frontend_image})
    args.manifest.write_text(json.dumps(updated, indent=2) + '\n')

if __name__ == '__main__': main()
