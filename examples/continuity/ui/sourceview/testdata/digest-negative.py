"""Run a real digest-bypass negative and exact unmodified-source restoration.

Usage: python3 digest-negative.py EVIDENCE_DIR RUN_CHECK
Run from the repository root with the same pinned isolated Go environment as
normal checks. The Go overlay affects only this package's production file.
"""
import hashlib
import json
from pathlib import Path
import subprocess
import sys

out = Path(sys.argv[1]).resolve()
guard = Path(sys.argv[2]).resolve()
source = Path('examples/continuity/ui/sourceview/sourceview.go').resolve()
original = source.read_bytes()
needle = b'if source.SHA256 != hex.EncodeToString(sum[:]) {'
assert original.count(needle) == 1
mutated = original.replace(needle, b'if source.SHA256 == "" && source.SHA256 != hex.EncodeToString(sum[:]) {')
mutant = out / 'digest-mutant.go'
mutant.write_bytes(mutated)
overlay = out / 'digest-overlay.json'
overlay.write_text(json.dumps({'Replace': {str(source): str(mutant)}}))
base = ['python3', str(guard), 'go', 'test', '-json', '-count=1', '-timeout=90s']
pkg = './examples/continuity/ui/sourceview'
negative = base + ['-overlay=' + str(overlay), '-run', '^TestInvalidDetail$/^wrong_digest$', pkg]
restored = base + [pkg]
results = []
for name, command in [('negative', negative), ('restored', restored)]:
    log = out / ('digest-' + name + '.log')
    with log.open('w') as f:
        result = subprocess.run(command, stdout=f, stderr=subprocess.STDOUT, timeout=120)
    events = []
    for line in log.read_text().splitlines():
        try:
            event = json.loads(line)
        except ValueError:
            continue
        if event.get('Action') in ('pass', 'fail', 'skip'):
            events.append(event)
    results.append({'name': name, 'command': command, 'exit': result.returncode, 'events': events})
    if name == 'negative':
        assert result.returncode != 0
        assert any(e.get('Test') == 'TestInvalidDetail/wrong_digest' and e['Action'] == 'fail' for e in events), 'wrong assertion failed'
        assert 'want nil/exact ErrInvalid' in log.read_text()
    else:
        assert result.returncode == 0
assert source.read_bytes() == original
sha = lambda b: hashlib.sha256(b).hexdigest()
(out / 'digest-evidence.json').write_text(json.dumps({'original_sha256': sha(original), 'mutated_sha256': sha(mutated), 'restored_sha256': sha(source.read_bytes()), 'method': 'Go overlay bypasses real body hash comparison; restoration removes overlay; actual source never changed.', 'results': results}, indent=2) + '\n')
