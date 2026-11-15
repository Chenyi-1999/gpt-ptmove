"""Run remote-only browser QA with credentials kept in process memory/stdin."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('mode', choices=['canary', 'readonly'])
    args = parser.parse_args()
    credential_reader = """import json
from pathlib import Path
values = dict(line.split('=', 1) for line in Path('/opt/gpt-ptmove/.env').read_text().splitlines() if '=' in line and not line.startswith('#'))
password = values.get('PTMOVE_ADMIN_PASSWORD')
if not password:
    raise SystemExit('Existing login unavailable; nothing changed')
username = json.loads(Path('/opt/gpt-ptmove-nexus/data/config.json').read_text()).get('auth', {}).get('username', 'admin')
print(json.dumps({'username': username, 'password': password}))
"""
    credentials = subprocess.run(['ssh', '-o', 'ConnectTimeout=10', 'itemkeep-server', 'python3', '-'],
                                 input=credential_reader, text=True, capture_output=True)
    if credentials.returncode:
        raise SystemExit('Could not read existing administrator login on152; no credentials printed')
    configuration = json.loads(credentials.stdout)
    credentials = None
    configuration.update(mode=args.mode, baseUrl='http://152.136.236.238:' + ('18121' if args.mode == 'canary' else '18120'))
    script = Path(__file__).with_name('browser_qa_api_management.cjs')
    result = subprocess.run([os.environ.get('QA_NODE', 'node'), str(script)], input=json.dumps(configuration), text=True)
    configuration.clear()
    return result.returncode


if __name__ == '__main__':
    sys.exit(main())
