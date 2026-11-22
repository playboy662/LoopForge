"""Workspace delivery gates: immutable handoffs and source hashes without Git."""
import hashlib
import json
from pathlib import Path
import re
import subprocess
import tarfile
import urllib.request
import urllib.parse
import uuid

ROOT = Path('.loopforge-workspace')
EXCLUDED = {'node_modules', '__pycache__', '.venv', 'target', 'build', '.runtime', '.data'}


def need(condition, message):
    if not condition:
        raise RuntimeError(message)


def text(value):
    return isinstance(value, str) and bool(value.strip())


def digest(value):
    return hashlib.sha256(json.dumps(value, ensure_ascii=False, sort_keys=True,
                                     separators=(',', ':')).encode()).hexdigest()


def read(path):
    return json.loads(Path(path).read_text())


def write(name, value):
    ROOT.mkdir(exist_ok=True)
    (ROOT / name).write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')


def archived(node, name):
    node_id = read(ROOT / 'nodes.json')[node]
    exact = Path('.agentflow/inputs') / node_id / name
    if exact.is_file():
        return read(exact)
    paths = [p for p in Path('.agentflow/inputs').glob('*/' + name)
             if p.parent.name.endswith('--' + node_id)]
    need(len(paths) == 1, '上游不可变产物缺失或来源不唯一：' + node + '/' + name)
    return read(paths[0])


def flow():
    return read(ROOT / 'flow.json')['flowId']


def same(report, keys=None):
    expected = current() if keys else {'flowId': flow()}
    keys = keys or ['flowId']
    need(all(report.get(k) == expected[k] for k in keys), '交付身份、方案或轮次不符')


def current():
    return archived('ready', 'current.json')


def identity(report):
    same(report, ['flowId', 'planDigest', 'roundId'])


def unique(rows, label):
    need(isinstance(rows, list) and rows and all(isinstance(x, dict) and text(x.get('id')) for x in rows), label + '缺少ID')
    need(len(rows) == len({x['id'] for x in rows}), label + 'ID重复')
    return {x['id']: x for x in rows}


def report(name):
    value = read(ROOT / (name + '.json'))
    same(value)
    return value


def document(name):
    path = ROOT / name
    need(path.is_file() and path.stat().st_size > 30, '交付正文缺失：' + name)
    return path.read_text()


def source_manifest():
    root = Path('app').resolve()
    need(root.is_dir() and not (root / '.git').exists(), '交付目录须为无Git的app工作区')
    result = {}
    for path in sorted(root.rglob('*')):
        relative = path.relative_to(root)
        need('.git' not in relative.parts, '本项目禁止引入Git元数据')
        if any(part in EXCLUDED for part in relative.parts):
            continue
        need(not path.is_symlink(), '应用交付不能包含符号链接')
        if path.is_file():
            need(path.stat().st_size < 10_000_000, '源码文件过大：' + str(relative))
            result[relative.as_posix()] = hashlib.sha256(path.read_bytes()).hexdigest()
    need(result, '应用源码为空')
    return result


def release():
    value = archived('development-check', 'release.json')
    identity(value)
    need(value['files'] == source_manifest(), '实际源码与已自测、审核的文件版本不符')
    return value


def plan():
    value = archived('plan-check', 'plan-package.json')
    same(value)
    unsigned = {k: v for k, v in value.items() if k != 'planDigest'}
    need(digest(unsigned) == value['planDigest'], '批准方案摘要不符')
    return value


def main(stage):
    ROOT.mkdir(exist_ok=True)
    if stage == 'init':
        if not (ROOT / 'flow.json').exists():
            write('flow.json', {'flowId': str(uuid.uuid4()), 'deliveryMode': 'workspace_v1'})
        need(not Path('app/.git').exists(), '禁止创建Git仓库')
    elif stage == 'product-check':
        product, ui = report('product'), report('ui')
        acceptance = unique(product.get('acceptance'), '产品验收')
        need(product.get('status') == ui.get('status') == 'ready' and product.get('openQuestions') == [], '产品或UI未完成')
        need(type(ui.get('uiRequired')) is bool, 'UI适用性未说明')
        if ui['uiRequired']:
            evidence = ui.get('interactionEvidence')
            need(isinstance(evidence, list) and all(isinstance(x, dict) and text(x.get('action')) and text(x.get('result')) for x in evidence), '缺原型真实交互证据')
            need({x.get('acceptanceId') for x in evidence} == set(acceptance), '原型交互未覆盖全部验收项')
        else:
            need(text(ui.get('reason')), 'UI不适用须说明原因')
        docs = {name: document(name + '.md') for name in ['product', 'ui-design']}
        if ui['uiRequired']:
            docs['prototype'] = document('prototype.html')
        package = {'flowId': flow(), 'product': product, 'ui': ui, 'documents': docs}
        package['productDigest'] = digest(package)
        write('product-package.json', package)
    elif stage == 'plan-check':
        product = archived('product-check', 'product-package.json')
        technical, database, testing = [report(name) for name in ['technical', 'database', 'test-plan']]
        for value in [technical, database, testing]:
            need(value.get('status') == 'ready' and value.get('productDigest') == product['productDigest'], '方案未就绪或产品来源错误')
            need(value.get('missing') == [], '方案仍有未确认的执行条件')
        need(technical.get('appRoot') == 'app' and isinstance(technical.get('apiContracts'), list), '应用目录或API契约未定义')
        need(all(text(technical.get('commands', {}).get(k)) for k in ['build', 'test']), '缺可执行构建/测试命令')
        deployment = technical.get('deployment', {})
        need(deployment.get('provider') == 'docker_compose' and text(deployment.get('composeFile')) and text(deployment.get('service')), '缺Compose部署定义')
        file = Path(deployment['composeFile'])
        need(not file.is_absolute() and '..' not in file.parts and file.parts[0] == 'app', 'Compose路径越界')
        need(type(deployment.get('hostPort')) is int and 1024 <= deployment['hostPort'] <= 65535, '缺已检查的候选端口')
        need(all(text(deployment.get(k)) for k in ['testUrl', 'healthUrl', 'versionUrl', 'environmentEvidence']), '缺真实测试/健康/版本入口及环境依据')
        need(type(database.get('changesRequired')) is bool, '数据库适用性缺失')
        if database['changesRequired']:
            need(all(text(database.get(k)) for k in ['engine', 'version', 'design', 'rollback']), '数据库设计缺失')
            document('database.sql')
        else:
            need(text(database.get('reason')), '无数据库须说明原因')
        cases = unique(testing.get('cases'), '测试用例')
        required = testing.get('requiredCaseIds')
        need(isinstance(required, list) and required and len(required) == len(set(required)) and set(required).issubset(cases), '必测项缺失或重复')
        aids = {x['id'] for x in product['product']['acceptance']}
        for case in cases.values():
            need(case.get('acceptanceId') in aids and isinstance(case.get('steps'), list) and case['steps'] and text(case.get('expected')), '用例未对应验收或缺步骤预期')
        need({cases[i]['acceptanceId'] for i in required} == aids, '必测未覆盖全部产品验收')
        package = {'flowId': flow(), 'product': product, 'technical': technical, 'database': database,
                   'testing': testing, 'documents': {name: document(name + '.md') for name in ['technical-plan', 'database-design', 'test-plan']}}
        package['planDigest'] = digest(package)
        write('plan-package.json', package)
    elif stage == 'ready':
        approved = plan()
        feedback = read(ROOT / 'feedback.json') if (ROOT / 'feedback.json').exists() else None
        write('current.json', {'flowId': flow(), 'planDigest': approved['planDigest'],
                               'roundId': str(uuid.uuid4()), 'feedback': feedback})
    elif stage == 'development-check':
        development = report('development')
        identity(development)
        need(development.get('status') == 'passed', '开发自测未通过')
        checks = []
        for kind, command in plan()['technical']['commands'].items():
            if kind not in ['build', 'test']:
                continue
            result = subprocess.run(command, shell=True, cwd='app', capture_output=True, text=True, timeout=600)
            checks.append({'kind': kind, 'command': command, 'exitCode': result.returncode,
                           'output': (result.stdout + result.stderr)[-12000:]})
            write('build-checks.json', checks)
            need(result.returncode == 0, '独立执行构建/测试失败：' + kind)
        files = source_manifest()
        write('release.json', {k: current()[k] for k in ['flowId', 'planDigest', 'roundId']} |
              {'sourceDigest': digest(files), 'files': files, 'checks': checks})
        archive = ROOT / 'source.tar.gz'
        with tarfile.open(archive, 'w:gz') as package:
            for name in files:
                package.add(Path('app') / name, arcname='app/' + name, recursive=False)
        need(archive.stat().st_size < 950000, '源码归档超过平台产物限制，须先调整产物交付方式')
    elif stage == 'review-check':
        version = release()
        reports = {}
        for name in ['code-review', 'database-review', 'ui-review']:
            value = report(name)
            identity(value)
            need(value.get('sourceDigest') == version['sourceDigest'] and value.get('status') in ['passed', 'rework'], '审核版本或结论无效')
            need(isinstance(value.get('findings'), list) and text(value.get('evidence')), '审核缺真实证据')
            need((value['status'] == 'passed') == (not value['findings']), '审核结论与缺陷不一致')
            for finding in value['findings']:
                need(all(text(finding.get(k)) for k in ['id', 'location', 'description', 'fixHint']), '缺陷不可执行')
            reports[name] = value
        write('reviews.json', {'flowId': flow(), 'sourceDigest': version['sourceDigest'], 'reports': reports,
                                'passed': all(v['status'] == 'passed' for v in reports.values())})
    elif stage == 'review-route':
        value = archived('review-check', 'reviews.json')
        if not value['passed']:
            write('feedback.json', {'kind': 'review', 'report': value})
            raise RuntimeError('专项审核发现缺陷，返回研发轮次处理；详见feedback.json')
    elif stage == 'deployment-check':
        version = release()
        reviews = archived('review-check', 'reviews.json')
        need(reviews.get('passed') is True and reviews.get('sourceDigest') == version['sourceDigest'], '部署缺本版本三项审核授权')
        value = report('deployment')
        identity(value)
        need(value.get('status') == 'passed' and value.get('sourceDigest') == version['sourceDigest'], '部署报告版本无效')
        deployment = plan()['technical']['deployment']
        container_id = value.get('containerId', '')
        image_id = value.get('imageId', '')
        need(isinstance(container_id, str) and re.fullmatch('[0-9a-f]{64}', container_id), '缺完整真实容器ID')
        need(isinstance(image_id, str) and re.fullmatch('sha256:[0-9a-f]{64}', image_id), '缺不可变镜像ID')
        result = subprocess.run(['docker', 'container', 'inspect', container_id], capture_output=True, text=True, timeout=30)
        need(result.returncode == 0, '无法查询实际容器')
        actual = json.loads(result.stdout)[0]
        labels = actual.get('Config', {}).get('Labels') or {}
        expected = {'com.docker.compose.project': 'lf-' + flow(), 'com.docker.compose.service': deployment['service'],
                    'io.loopforge.flow-id': flow(), 'io.loopforge.round-id': current()['roundId'],
                    'io.loopforge.source-digest': version['sourceDigest']}
        need(actual.get('Id') == container_id and actual.get('Image') == image_id and
             actual.get('State', {}).get('Running') is True and actual['State'].get('Health', {}).get('Status') == 'healthy', '实际容器不健康或镜像不符')
        need(all(labels.get(k) == v for k, v in expected.items()), '实际容器交付身份/轮次/源码标签不符')
        need(all(text(value.get(k)) for k in ['executionEvidence', 'healthEvidence', 'versionEvidence', 'testUrl']), '缺真实部署证据')
        need(value['testUrl'] == deployment['testUrl'], '部署测试入口与方案不符')
        for key in ['healthUrl', 'versionUrl']:
            path = urllib.parse.urlsplit(deployment[key]).path
            url = 'http://127.0.0.1:' + str(deployment['hostPort']) + path
            with urllib.request.urlopen(url, timeout=15) as response:
                payload = response.read(100000).decode()
                need(response.status == 200, '应用HTTP检查失败')
            if key == 'versionUrl':
                need(json.loads(payload).get('sourceDigest') == version['sourceDigest'], '实际HTTP版本与审核源码不符')
        write('deployed.json', value)
    elif stage == 'qa-check':
        version = release()
        value = report('qa')
        identity(value)
        need(value.get('sourceDigest') == version['sourceDigest'] and value.get('status') == 'completed', 'QA版本或完成状态无效')
        deployed = archived('operations', 'deployed.json')
        identity(deployed)
        need(deployed.get('sourceDigest') == version['sourceDigest'], 'QA未使用本轮已核验的部署')
        with urllib.request.urlopen(plan()['technical']['deployment']['versionUrl'], timeout=15) as response:
            actual_version = json.loads(response.read(100000))
        need(actual_version.get('sourceDigest') == version['sourceDigest'], 'QA入口的实际版本漂移')
        required = set(plan()['testing']['requiredCaseIds'])
        cases = unique(value.get('cases'), 'QA结果')
        need(required.issubset(cases), 'QA缺少必测项')
        for case in cases.values():
            need(case.get('status') in ['passed', 'failed'] and text(case.get('evidence')), '用例缺真实执行证据或被跳过')
        failed = any(cases[i]['status'] != 'passed' for i in required)
        need(isinstance(value.get('defects'), list) and failed == bool(value['defects']), 'QA结论与缺陷不一致')
        need(value.get('quality') == ('failed' if failed else 'passed'), 'QA质量结论不一致')
        write('qa-result.json', value)
    elif stage == 'qa-route':
        value = archived('qa-check', 'qa-result.json')
        if value['quality'] != 'passed':
            write('feedback.json', {'kind': 'qa', 'report': value})
            raise RuntimeError('QA发现业务缺陷，返回研发轮次处理；详见feedback.json')
    elif stage == 'delivery':
        version = release()
        qa = archived('qa-check', 'qa-result.json')
        identity(qa)
        need(qa['quality'] == 'passed' and qa['sourceDigest'] == version['sourceDigest'], '验收未通过')
        deployment = archived('operations', 'deployed.json')
        identity(deployment)
        need(deployment['sourceDigest'] == version['sourceDigest'], '部署版本与验收不符')
        write('delivery.json', {'flowId': flow(), 'sourceDigest': version['sourceDigest'],
                                'status': 'passed', 'deployment': deployment, 'qa': qa})
    else:
        raise ValueError('未知阶段：' + stage)
    print('工作区门禁通过：' + stage)
