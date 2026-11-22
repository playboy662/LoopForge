"""Compose-specific plan and runtime checks embedded into workflow script gates."""
import json
import re
import subprocess
from pathlib import PurePosixPath


def compose_plan(unit, repository_ids):
    docker = unit.get('docker')
    if not isinstance(docker, dict) or docker.get('composeRepoId') not in repository_ids:
        raise RuntimeError('Docker 部署单元缺批准的 Compose 仓库')
    file = docker.get('composeFile')
    if not isinstance(file, str) or not file.strip() or PurePosixPath(file).is_absolute() or '..' in PurePosixPath(file).parts:
        raise RuntimeError('Compose 文件路径越界或缺失')
    if not isinstance(docker.get('service'), str) or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]*', docker['service']):
        raise RuntimeError('Compose 服务名无效')
    return docker


def compose_runtime(unit, report, flow_id, round_id):
    docker = report.get('docker')
    project = 'lf-' + flow_id
    if not isinstance(docker, dict) or docker.get('projectName') != project or docker.get('service') != unit['docker']['service']:
        raise RuntimeError('Docker 项目或服务不是本轮批准范围')
    container_id = docker.get('containerId', '')
    image_id = docker.get('imageId', '')
    if not isinstance(container_id, str) or not re.fullmatch(r'[0-9a-f]{64}', container_id):
        raise RuntimeError('缺真实完整容器 ID')
    if not isinstance(image_id, str) or not re.fullmatch(r'sha256:[0-9a-f]{64}', image_id) or report.get('version') != image_id:
        raise RuntimeError('部署版本必须为真实 Docker 镜像 ID')
    result = subprocess.run(['docker', 'container', 'inspect', container_id], capture_output=True, text=True, timeout=30)
    if result.returncode:
        raise RuntimeError('无法从实际 Docker daemon 核对本轮容器')
    containers = json.loads(result.stdout)
    if not isinstance(containers, list) or len(containers) != 1:
        raise RuntimeError('Docker 查询未返回唯一容器')
    container = containers[0]
    labels = container.get('Config', {}).get('Labels') or {}
    state = container.get('State', {})
    if container.get('Id') != container_id or container.get('Image') != image_id or state.get('Running') is not True or state.get('Health', {}).get('Status') != 'healthy':
        raise RuntimeError('容器未运行健康或实际镜像与报告不符')
    expected = {'com.docker.compose.project': project, 'com.docker.compose.service': unit['docker']['service'],
                'io.loopforge.flow-id': flow_id, 'io.loopforge.round-id': round_id}
    if unit['action'] == 'changed':
        if report.get('builtSha') != report.get('mergeSha') or report.get('deployedSha') != report.get('builtSha'):
            raise RuntimeError('构建必须基于精确合并 SHA')
        expected['org.opencontainers.image.revision'] = report['builtSha']
    if any(labels.get(key) != value for key, value in expected.items()):
        raise RuntimeError('实际容器标签不属于本次交付身份、轮次或合并版本')
    if not isinstance(report.get('executionEvidence'), str) or not report['executionEvidence'].strip():
        raise RuntimeError('缺 Docker 构建与启动执行记录')
