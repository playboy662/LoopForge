"""Verify a flow's Compose publication and toolchain HTTP route, then clean up."""
import datetime
import json
from pathlib import Path
import re
import secrets
import socket
import subprocess
import tempfile
import time
import urllib.request


def command(arguments, timeout=45):
    result = subprocess.run(arguments, capture_output=True, text=True, timeout=timeout)
    if result.returncode:
        raise RuntimeError('Compose路由预检命令失败：' + arguments[0] + ' ' + result.stderr[-500:])
    return result.stdout.strip()


def verify(flow_id, host_port, toolchain):
    if not re.fullmatch('[0-9a-f-]{36}', flow_id) or not 1024 <= host_port <= 65535:
        raise ValueError('交付身份或候选端口无效')
    output = Path('.loopforge-environment-preflight')
    output.mkdir(exist_ok=True)
    project = 'lf-' + flow_id + '-route-probe'
    marker = secrets.token_hex(24)
    def available():
        # Completed HTTP connections may remain in TIME_WAIT on macOS. Reuse
        # permits that state, but still refuses another active listener.
        for attempt in range(30):
            try:
                with socket.socket() as listener:
                    listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                    listener.bind(('127.0.0.1', host_port))
                return
            except OSError:
                if attempt == 29:
                    raise
                time.sleep(0.1)

    available()
    docker_version = command(['docker', 'info', '--format', '{{.ServerVersion}}'])
    compose_version = command(['docker', 'compose', 'version', '--short'])
    image_id = command(['docker', 'image', 'inspect', toolchain, '--format', '{{.Id}}'])
    with tempfile.TemporaryDirectory(prefix='compose-route-', dir=output.resolve()) as scratch:
        directory = Path(scratch)
        (directory / 'index.html').write_text(marker)
        config = {'services': {'probe': {
            'image': toolchain, 'pull_policy': 'never', 'entrypoint': ['python3'],
            'command': ['-m', 'http.server', '8080', '--bind', '0.0.0.0', '--directory', '/probe'],
            'ports': ['127.0.0.1:' + str(host_port) + ':8080'],
            'volumes': [str(directory) + ':/probe:ro'],
            'labels': {'io.loopforge.flow-id': flow_id, 'io.loopforge.purpose': 'route-preflight'},
            'read_only': True}}}
        path = directory / 'compose.json'
        path.write_text(json.dumps(config))
        compose = ['docker', 'compose', '-p', project, '-f', str(path)]
        command(compose + ['config', '--quiet'])
        try:
            command(compose + ['up', '-d', '--no-build'])
            local_url = 'http://127.0.0.1:' + str(host_port) + '/'
            for attempt in range(20):
                try:
                    with urllib.request.urlopen(local_url, timeout=3) as response:
                        if response.read().decode() == marker:
                            break
                except OSError:
                    pass
                time.sleep(0.5)
            else:
                raise RuntimeError('Compose宿主发布没有读回随机标记')
            test_url = 'http://host.docker.internal:' + str(host_port) + '/'
            probe = 'import urllib.request; assert urllib.request.urlopen(' + repr(test_url) + ',timeout=10).read().decode()==' + repr(marker)
            command(['docker', 'run', '--rm', '--entrypoint', 'python3', toolchain, '-c', probe])
            containers = command(compose + ['ps', '-q', 'probe']).splitlines()
            if len(containers) != 1:
                raise RuntimeError('Compose探针容器不唯一')
            container = json.loads(command(['docker', 'container', 'inspect', containers[0]]))[0]
            labels = container['Config']['Labels']
            if not container['State']['Running'] or labels.get('io.loopforge.flow-id') != flow_id:
                raise RuntimeError('探针实际容器身份或状态不符')
            container_id = container['Id']
        finally:
            command(compose + ['down', '--remove-orphans'])
    available()
    report = {'flowId': flow_id, 'status': 'passed',
              'observedAt': datetime.datetime.now(datetime.timezone.utc).isoformat(),
              'provider': 'docker_compose', 'dockerVersion': docker_version,
              'composeVersion': compose_version, 'hostPort': host_port,
              'hostBind': '127.0.0.1', 'userUrl': local_url, 'testUrl': test_url,
              'toolchain': toolchain, 'toolchainImageId': image_id,
              'probeContainerId': container_id, 'composeProject': project,
              'evidence': '原生Compose实际发布；宿主和独立工具链容器均读回随机标记',
              'cleanup': 'Compose探针容器及网络已删除，临时目录已删除，端口重新绑定后释放',
              'limits': '这是环境预检；业务健康和版本在批准方案后的实际部署核验，部署前必须重查端口。'}
    (output / 'compose-route.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
    return report
