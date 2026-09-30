#!/usr/bin/env python3
"""Validate planning structure; this does not verify product implementation."""
import json
import re
import sys
import argparse
from pathlib import Path

ROOT=Path(__file__).resolve().parents[1]
errors=[]
def check(ok,message):
    if not ok: errors.append(message)

def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--data', type=Path, default=ROOT/'docs/planning/plan-data.json')
    args=parser.parse_args()
    d=json.loads(args.data.read_text())
    epics=d['epics']; ts=[t for e in epics for t in e['tasks']]; us=d['use_cases']
    progress_path=ROOT/'docs/planning/execution-state.json'
    progress=json.loads(progress_path.read_text()) if progress_path.exists() else {}
    ids=[t['id'] for t in ts]; uids=[u['id'] for u in us]; lookup={t['id']:t for t in ts}
    check(len(ids)==len(set(ids)),'Duplicate task ID')
    check(len(uids)==len(set(uids)),'Duplicate use-case ID')
    for t in ts:
        for field in ['objective','owned_paths','instructions','acceptance','verification','negative_test','use_cases','stage','lane']:
            check(bool(t.get(field)),f"{t['id']}: missing {field}")
        check(30<=t['estimate_minutes']<=90,f"{t['id']}: estimate outside granular range")
        check(t['lane'] in [f'L{i:02}' for i in range(1,17)],f"{t['id']}: invalid lane")
        for dep in t['deps']:
            check(dep in lookup,f"{t['id']}: missing dependency {dep}")
            if dep in lookup:
                check(lookup[dep]['wave']<t['wave'],f"{t['id']}: dependency not in earlier wave: {dep}")
                check(lookup[dep]['stage']<=t['stage'],f"{t['id']}: later-stage dependency: {dep}")
        for u in t['use_cases']: check(u in uids,f"{t['id']}: unknown use case {u}")
        p=ROOT/f"docs/tasks/{t['id']}.md"
        check(p.is_file(),f"Missing task contract {t['id']}")
        if p.exists():
            text=p.read_text()
            for h in ['## Objective','## Scope','## Acceptance criteria','## Verification','## Negative verification','Certification: ']:
                check(h in text,f"{t['id']}: missing contract section {h}")
            check(t['title'] in text,f"{t['id']}: stale contract title")
        check(not any('REQUIRES:' in str(v) for v in t.values()),f"{t['id']}: unresolved semantic dependency")
    for tid,state in progress.items():
        check(tid in lookup, f'Unknown execution task {tid}')
        check(state.get('status') in {'PLANNED','IN_PROGRESS','BLOCKED','ACCEPTED'},f'{tid}: invalid execution status')
        if state.get('status')=='ACCEPTED':
            check(bool(state.get('evidence')),f'{tid}: acceptance requires evidence')
            check(state.get('certification')=='REVIEWED',f'{tid}: acceptance requires reviewed certification')
            if tid in lookup:
                for dep in lookup[tid]['deps']:
                    check(progress.get(dep,{}).get('status')=='ACCEPTED',f'{tid}: unaccepted dependency {dep}')
    for u in uids: check(any(u in t['use_cases'] for t in ts),f"Uncovered use case {u}")
    seen=[]
    for w in d['waves']:
        check(0<len(w['tasks'])<=16,f"Wave {w['id']}: invalid size")
        lanes=[lookup[i]['lane'] for i in w['tasks']]
        check(len(lanes)==len(set(lanes)),f"Wave {w['id']}: multiple writers in lane")
        for i,a in enumerate(w['tasks']):
            for b in w['tasks'][i+1:]:
                for left in lookup[a]['owned_paths']:
                    for right in lookup[b]['owned_paths']:
                        left=left.rstrip('/*');right=right.rstrip('/*')
                        check(not(left==right or left.startswith(right+'/') or right.startswith(left+'/')),f"Wave {w['id']}: overlapping owned paths for {a}/{b}")
        seen+=w['tasks']
    check(sorted(seen)==sorted(ids),'Wave inventory differs from tasks')
    for e in epics:
        p=ROOT/f"docs/plans/{e['id']}.md"
        check(p.exists(),f"Missing epic {e['id']}")
        if p.exists():
            marks=re.findall(r'^- \[[ x]\] (T\d+\.\d+) ',p.read_text(),re.M)
            check(marks==[t['id'] for t in e['tasks']],f"Epic task inventory mismatch {e['id']}")
    for p in (ROOT/'docs').rglob('*.md'):
        text=p.read_text()
        check(not re.search(r'/Users/|/home/|file://|\bAKIA[A-Z0-9]{16}\b',text),f"Potential private path/credential in {p.relative_to(ROOT)}")
        for target in re.findall(r'\]\(([^)]+)\)',text):
            if '://' in target or target.startswith('#'):continue
            dest=(p.parent/target.split('#')[0]).resolve()
            check(dest.exists(),f"Broken link {p.relative_to(ROOT)} -> {target}")
    check((ROOT/'docs/usecases-manifest.json').exists(),'Missing canonical manifest')
    reached=set()
    def visit(i):
        if i in reached or i not in lookup:return
        reached.add(i)
        for dep in lookup[i]['deps']:visit(dep)
    visit('T16.12')
    check(reached==set(ids),'Full release gate does not reach every planned task')
    if errors:
        print('\n'.join(errors));return 1
    print(f"PASS: {len(epics)} epics, {len(ts)} tasks, {len(us)} covered use cases, {len(d['waves'])} dependency-ordered waves, peak {max(len(w['tasks']) for w in d['waves'])} slots. Planning checks only; no product execution certified.")
    return 0

if __name__=='__main__':sys.exit(main())
