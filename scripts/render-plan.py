#!/usr/bin/env python3
"""Render the complete current inventory into a new directory, or check without writes."""
import argparse
import importlib.util
from pathlib import Path
import sys


def load_checker():
    spec = importlib.util.spec_from_file_location('amos_current_plan', Path(__file__).with_name('check-plan.py'))
    module = importlib.util.module_from_spec(spec)
    sys.dont_write_bytecode = True
    spec.loader.exec_module(module)
    return module


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--data', type=Path, help='current-source candidate; retired baseline rejected')
    parser.add_argument('--check', action='store_true', help='validate without writes; compare output if supplied')
    parser.add_argument('--output-dir', type=Path, help='new directory to create; existing directories are never overwritten')
    parser.add_argument('--native-markdown', action='store_true', help='stage discoverable docs/plan.md and docs/plans/*.md; no in-place writes')
    args = parser.parse_args()
    if not args.check and not args.output_dir:
        parser.error('provide --check or --output-dir; in-place rendering is forbidden')
    checker = load_checker()
    try:
        current = checker.load_current(args.data)
        files = (checker.native_projection_files(current) if args.native_markdown
                 else checker.projection_files(current))
        if args.check:
            if args.output_dir:
                if args.native_markdown:
                    checker.check_native_projection(args.output_dir, current)
                else:
                    checker.check_projection(args.output_dir, files)
            print('PASS: current projection ' + ('freshness' if args.output_dir else 'generation (in memory only)') +
                  '; no files written and no release qualified.')
        else:
            # mkdir is exclusive: no existing directory, file or symlink may be reused.
            args.output_dir.mkdir(parents=False, exist_ok=False)
            for name, content in files.items():
                target = args.output_dir / name
                target.parent.mkdir(parents=True, exist_ok=True)
                with target.open('xb') as stream:
                    stream.write(content)
            print('Rendered current tasks; registries preserved as unqualified records. No release qualified.')
        return 0
    except (OSError, ValueError, TypeError, RecursionError) as exc:
        print('ERROR: current projection failed: ' + str(exc), file=sys.stderr)
        return 2


if __name__ == '__main__':
    sys.exit(main())
