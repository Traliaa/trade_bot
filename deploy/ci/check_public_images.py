#!/usr/bin/env python3
"""Gate promotion on anonymous manifest access; cluster registry secrets are unnecessary."""
import json
import os
import re
import urllib.request


def main():
    for role in ('backend', 'frontend'):
        digest = os.environ[role.upper() + '_DIGEST']
        if not re.fullmatch(r'sha256:[0-9a-f]{64}', digest):
            raise SystemExit('INVALID_IMAGE_DIGEST: ' + role)
        repo = 'traliaa/trade-bot-' + role
        try:
            with urllib.request.urlopen('https://ghcr.io/token?service=ghcr.io&scope=repository:' + repo + ':pull', timeout=20) as response:
                token = json.load(response)['token']
            request = urllib.request.Request('https://ghcr.io/v2/' + repo + '/manifests/' + digest, headers={
                'Authorization': 'Bearer ' + token,
                'Accept': 'application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json',
            })
            with urllib.request.urlopen(request, timeout=20) as response:
                if response.headers.get('Docker-Content-Digest') != digest:
                    raise ValueError('digest mismatch')
        except Exception:
            raise SystemExit('ANONYMOUS_PULL_FAILED: ' + repo + '; verify package visibility and availability') from None
        print('ANONYMOUS_MANIFEST_VERIFIED', repo, digest)

if __name__ == '__main__': main()
