"""Start and remove one isolated Compose probe through the executor's Docker."""
import json
import subprocess
import tempfile
import uuid
from pathlib import Path

image = 'alpine:3.21'
project = 'loopforge-envcheck-' + uuid.uuid4().hex[:12]


def run(command, timeout=60):
    result = subprocess.run(command, text=True, capture_output=True, timeout=timeout)
    output = (result.stdout + result.stderr).strip()
    print(json.dumps({'command': command, 'exitCode': result.returncode, 'output': output[-2500:]}, ensure_ascii=False), flush=True)
    if result.returncode:
        raise RuntimeError('执行失败：' + command[0])
    return output


run(['loopforge-docker-check'])
run(['docker', 'version', '--format', '{{.Server.Version}}'])
run(['docker', 'compose', 'version'])
existed = subprocess.run(['docker', 'image', 'inspect', image], capture_output=True).returncode == 0
with tempfile.TemporaryDirectory(prefix='.loopforge-docker-check-', dir=Path.cwd()) as directory:
    compose = Path(directory) / 'compose.yaml'
    compose.write_text('services:\n  probe:\n    image: alpine:3.21\n    command: ["/bin/sh", "-c", "printf LOOPFORGE_COMPOSE_OK"]\n    network_mode: none\n    read_only: true\n')
    command = ['docker', 'compose', '-p', project, '-f', str(compose)]
    try:
        run(command + ['config', '--quiet'])
        output = run(command + ['up', '--abort-on-container-exit', '--exit-code-from', 'probe'])
        if 'LOOPFORGE_COMPOSE_OK' not in output:
            raise RuntimeError('容器未产生预期的真实执行输出')
    finally:
        run(command + ['down', '--remove-orphans'])
        if not existed:
            # Do not force removal if another task started using this image.
            result = subprocess.run(['docker', 'image', 'rm', image], text=True, capture_output=True)
            print(json.dumps({'temporaryImageRemoved': result.returncode == 0}, ensure_ascii=False), flush=True)
print(json.dumps({'passed': True, 'scope': '真实 Docker Compose 启动、执行和清理；尚不代表业务交付通过'}, ensure_ascii=False))
