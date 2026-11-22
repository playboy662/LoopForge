"""Verify real executables in the selected LoopForge sandbox without compiling."""
import json
import re
import subprocess

checks = [
    ('java', ['java', '-version'], r'version "21[.\"]'),
    ('maven', ['mvn', '--version'], r'Apache Maven 3\.9\.11'),
    ('node', ['node', '--version'], r'v22\.'),
    ('npm', ['npm', '--version'], r'\d+\.\d+\.\d+'),
    ('python', ['python3', '--version'], r'Python 3\.'),
    ('mysql-client', ['mysql', '--version'], r'(?:mysql|mariadb)'),
    ('codex', ['codex', '--version'], r'codex-cli 0\.171\.0'),
    ('git', ['git', '--version'], r'git version'),
    ('ripgrep', ['rg', '--version'], r'ripgrep'),
]
results = []
for name, command, expected in checks:
    result = subprocess.run(command, text=True, capture_output=True, timeout=30)
    output = (result.stdout + result.stderr).strip()
    passed = result.returncode == 0 and re.search(expected, output, re.IGNORECASE) is not None
    results.append({'tool': name, 'command': command, 'exitCode': result.returncode,
                    'passed': passed, 'output': output[:1200]})
    print(json.dumps(results[-1], ensure_ascii=False), flush=True)
    if not passed:
        raise SystemExit('工具版本或执行失败：' + name)
print(json.dumps({'passed': len(results), 'scope': '真实沙箱工具链，尚不代表业务交付通过'}, ensure_ascii=False))
