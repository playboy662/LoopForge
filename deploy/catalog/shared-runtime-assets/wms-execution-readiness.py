"""Prepare flow-scoped execution resources without rewriting an approved design."""
import datetime
import http.client
import http.server
import json
import os
from pathlib import Path
import re
import secrets
import socket
import subprocess
import threading
import time
import urllib.parse

TOOLCHAIN = 'agentflow-img-d38a42aa845fc7ae'
MYSQL = 'mysql:8.4.11@sha256:0f51e63e537a1f176423fa03ef559aabe6fa4372a53160a44bc7e75da7d15364'
ALLOWED = {'playboy662/c12-work-test-biz', 'playboy662/c12-work-test-ui'}
EXPECTED = {'technical': {'M1', 'M2', 'M3', 'M4', 'M5', 'M6', 'M7'},
            'database': {'M3', 'M6', 'M7', 'D1'},
            'testPlan': {'M1', 'M2', 'M3', 'M4', 'M5', 'M6', 'M7', 'D1', 'Q1', 'Q2', 'Q3'}}


def need(condition, message):
    if not condition:
        raise RuntimeError(message)


def condition_ids(plan):
    result = set()
    for name, allowed in EXPECTED.items():
        missing = plan[name].get('missing', [])
        need(isinstance(missing, list), name + '.missing必须是数组')
        for item in missing:
            match = re.match(r'^([MDQ]\d+)\s', item) if isinstance(item, str) else None
            need(match and match[1] in allowed, '未登记的执行条件，须重新评审：' + str(item))
            result.add(match[1])
    return result


def run(arguments, *, timeout=90, data=None, env=None):
    result = subprocess.run(arguments, input=data, capture_output=True, text=True,
                            timeout=timeout, env=env)
    diagnostic = result.stderr
    for name in ['AGENTFLOW_GIT_TOKEN', 'GITHUB_TOKEN', 'GH_TOKEN']:
        if os.environ.get(name):
            diagnostic = diagnostic.replace(os.environ[name], '[已隐藏]')
    diagnostic = re.sub(r'https?://[^\s/@]+:[^\s/@]+@', 'https://[已隐藏]@', diagnostic)
    diagnostic = re.sub(r'(?:github_pat_|gh[pousr]_)[A-Za-z0-9_]+', '[已隐藏]', diagnostic)
    need(result.returncode == 0, '执行预检失败（退出' + str(result.returncode) + '）：'
         + str(arguments[0]) + ' ' + diagnostic[:600])
    return result.stdout.strip()


def save(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')
    path.chmod(0o600)


def github(path, token):
    connection = http.client.HTTPSConnection('api.github.com', timeout=20)
    try:
        connection.request('GET', path, headers={'Authorization': 'Bearer ' + token,
                           'Accept': 'application/vnd.github+json', 'User-Agent': 'LoopForge-readiness'})
        response = connection.getresponse()
        body = response.read(1_000_001)
        need(response.status == 200 and len(body) <= 1_000_000,
             'GitHub权限预检失败，HTTP ' + str(response.status))
        return json.loads(body)
    finally:
        connection.close()


def private_file(directory, name):
    path = directory / name
    need(not path.is_symlink(), '私密文件不能是符号链接')
    if not path.exists():
        descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(descriptor, 'w') as stream:
            stream.write(secrets.token_urlsafe(30) + '\n')
    path.chmod(0o600)
    need(path.stat().st_size > 20, '私密文件内容无效')
    return path


def prepare(plan):
    pending = condition_ids(plan)
    flow = plan['flowId']
    need(re.fullmatch('[0-9a-f-]{36}', flow), '无效交付身份')
    root = Path.cwd().resolve()
    directory = root / '.loopforge-delivery'
    private = directory / 'private'
    private.mkdir(mode=0o700, exist_ok=True)
    need(not private.is_symlink(), '私密目录不能是符号链接')
    private.chmod(0o700)
    result = {'flowId': flow, 'planDigest': plan['planDigest'], 'status': 'preparing',
              'checks': {}, 'deferred': {'Q1': '部署与版本门禁', 'Q2': '开发自测及QA必测门禁',
                                       'Q3': '本轮current.feedback与QA缺陷门禁'}}

    def record(identifier, evidence):
        result['checks'][identifier] = {'status': 'passed', 'evidence': evidence}
        save(directory / 'environment-ready.json', result)
        print('执行环境预检 ' + identifier + ' 通过', flush=True)

    # Permission probes use the same injected identity as later delivery nodes.
    token = os.environ.get('AGENTFLOW_GIT_TOKEN', '').strip()
    need(token, '执行机未注入仓库凭据')
    repositories = []
    for repository in plan['technical']['repositories']:
        if repository['access'] != 'change':
            continue
        checkout = (root / repository['repoPath']).resolve()
        need(checkout.is_relative_to(root), '仓库目录越界')
        origin = run(['git', '-C', str(checkout), 'remote', 'get-url', 'origin'])
        parsed = urllib.parse.urlsplit(origin)
        slug = parsed.path.strip('/').removesuffix('.git')
        need(parsed.hostname == 'github.com' and slug in ALLOWED, '仓库不在已批准WMS范围')
        details = github('/repos/' + slug, token)
        need(details.get('permissions', {}).get('push') is True, '本轮仓库身份无写权限：' + slug)
        branch = github('/repos/' + slug + '/branches/test', token)
        need(branch.get('protected') is False, 'test受保护，需明确核对合并规则：' + slug)
        github('/repos/' + slug + '/pulls?state=open&per_page=1', token)
        askpass = private / 'git-askpass.py'
        askpass.write_text('#!/usr/bin/env python3\nimport os,sys\nprint("x-access-token" if "Username" in sys.argv[1] else os.environ["AGENTFLOW_GIT_TOKEN"])\n')
        askpass.chmod(0o700)
        git_env = dict(os.environ, GIT_ASKPASS=str(askpass), GIT_TERMINAL_PROMPT='0')
        run(['git', '-c', 'credential.helper=', '-c', 'core.hooksPath=/dev/null',
             '-C', str(checkout), 'push', '--dry-run', 'origin',
             'HEAD:refs/heads/' + repository['workBranch']], timeout=45, env=git_env)
        identifier = 'M1' if slug.endswith('-biz') else 'M2'
        record(identifier, {'repository': slug, 'pushDryRun': True, 'pullsReadable': True,
                            'testProtected': False, 'testSha': branch['commit']['sha']})
        repositories.append({'repoId': repository['id'], 'repository': slug,
                             'deliveryBranch': 'test', 'observedSha': branch['commit']['sha']})
    need(len(repositories) == 2, 'WMS双仓库权限未覆盖')

    run(['docker', 'info', '--format', '{{.ServerVersion}}'])
    run(['docker', 'compose', 'version', '--short'])
    run(['docker', 'image', 'inspect', TOOLCHAIN, '--format', '{{.Id}}'])
    mysql_source = MYSQL
    inspect = subprocess.run(['docker', 'image', 'inspect', mysql_source], capture_output=True, text=True)
    if inspect.returncode:
        # Explicit adapter-local mirror, same immutable official manifest digest.
        # Docker validates the content digest; global daemon settings stay intact.
        mysql_source = 'mirror.gcr.io/library/mysql@' + MYSQL.split('@', 1)[1]
        print('按批准的精确摘要从镜像缓存准备MySQL', flush=True)
        cached = subprocess.run(['docker', 'image', 'inspect', mysql_source], capture_output=True, text=True)
        if cached.returncode:
            run(['docker', 'pull', mysql_source], timeout=300)
    image = json.loads(run(['docker', 'image', 'inspect', mysql_source]))[0]
    need(any(x == 'MYSQL_VERSION=8.4.11-1.el9' for x in image['Config']['Env']), 'MySQL版本与批准设计不符')
    dependency_probe = "import urllib.request; [urllib.request.urlopen(u,timeout=20).close() for u in ['https://repo.maven.apache.org/maven2/','https://registry.npmjs.org/vue']]; print('dependency registries reachable')"
    run(['docker', 'run', '--rm', '--entrypoint', 'python3', TOOLCHAIN, '-c', dependency_probe], timeout=60)
    record('M3', {'mysqlImageId': image['Id'], 'mysqlReference': MYSQL, 'availableMysqlReference': mysql_source,
                   'toolchain': TOOLCHAIN, 'dependencyRegistries': 'Maven Central/npm实际HTTP请求通过'})

    # Probe the approved QA route, then release it; deployment must recheck it.
    host_ports = {urllib.parse.urlsplit(u['testUrl']).port for u in plan['technical']['deployables']}
    need(len(host_ports) == 1 and None not in host_ports, 'WMS入口端口不一致')
    port = next(iter(host_ports))
    marker = secrets.token_hex(16)

    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            self.send_response(200)
            self.end_headers()
            self.wfile.write(marker.encode())

        def log_message(self, *args):
            pass

    server = http.server.ThreadingHTTPServer(('127.0.0.1', port), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        probe = 'import urllib.request; assert urllib.request.urlopen(' + repr('http://host.docker.internal:' + str(port)) + ',timeout=10).read().decode()==' + repr(marker)
        run(['docker', 'run', '--rm', '--entrypoint', 'python3', TOOLCHAIN, '-c', probe], timeout=30)
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)
    record('M4', {'hostPort': port, 'bind': '127.0.0.1', 'qaRoute': 'host.docker.internal',
                  'probe': '同工具链沙箱实际HTTP读回随机标记；已停止探针并释放端口，部署前必须重查'})

    ledger = directory / 'merge-ledger'
    ledger.mkdir(mode=0o700, exist_ok=True)
    ledger.chmod(0o700)
    ledger_file = ledger / 'state.json'
    if not ledger_file.exists():
        save(ledger_file, {'flowId': flow, 'preflight': repositories, 'merges': []})
    need(json.loads(ledger_file.read_text())['flowId'] == flow, '合并记录属于其他flow')
    record('M5', {'ledgerPath': ledger_file.relative_to(root).as_posix(),
                  'recovery': '同flow持久工作区读回成功；运维逐仓库写入合并意图与实际结果，不得记录推测成功'})

    secret_refs = {}
    for name in ['mysql-root', 'mysql-app', 'mysql-migrate', 'mysql-it', 'admin-password']:
        secret_refs[name] = private_file(private, name).relative_to(root).as_posix()
    record('M6', {'privateDirectory': private.relative_to(root).as_posix(), 'mode': '0700/0600',
                  'secretRefs': secret_refs, 'handoff': '当前flow共享工作区；只交接相对文件引用，禁止输出或归档口令'})

    container_name = 'lf-' + flow + '-it-mysql'
    existing = subprocess.run(['docker', 'container', 'inspect', container_name], capture_output=True, text=True)
    if existing.returncode == 0:
        container = json.loads(existing.stdout)[0]
        need(container['Config'].get('Labels', {}).get('io.loopforge.flow-id') == flow
             and container['Image'] == image['Id'], '现有测试库身份或版本不符')
        ports = container['HostConfig']['PortBindings']['3306/tcp']
        need(len(ports) == 1 and ports[0]['HostIp'] == '127.0.0.1', '测试库发布范围不符')
        database_port = int(ports[0]['HostPort'])
        if not container['State']['Running']:
            run(['docker', 'start', container_name])
    else:
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', 0))
            database_port = listener.getsockname()[1]
        run(['docker', 'run', '-d', '--name', container_name,
             '--label', 'io.loopforge.flow-id=' + flow, '--label', 'io.loopforge.purpose=integration-test',
             '-p', '127.0.0.1:' + str(database_port) + ':3306',
             '--mount', 'type=bind,src=' + str(private / 'mysql-root') + ',dst=/run/secrets/mysql-root,readonly',
             '--mount', 'type=bind,src=' + str(private / 'mysql-it') + ',dst=/run/secrets/mysql-it,readonly',
             '-e', 'MYSQL_ROOT_PASSWORD_FILE=/run/secrets/mysql-root', '-e', 'MYSQL_DATABASE=rbac_it',
             '-e', 'MYSQL_USER=rbac_it', '-e', 'MYSQL_PASSWORD_FILE=/run/secrets/mysql-it',
             mysql_source], timeout=60)
    client_env = private / 'integration-client.env'
    client_env.write_text('MYSQL_PWD=' + (private / 'mysql-it').read_text().strip() + '\n')
    client_env.chmod(0o600)
    command = ['docker', 'run', '--rm', '--env-file', str(client_env), '--entrypoint', 'mysql',
               TOOLCHAIN, '--protocol=tcp', '-h', 'host.docker.internal', '-P', str(database_port),
               '-u', 'rbac_it', '-D', 'rbac_it', '-N', '-e', 'SELECT VERSION(),1']
    connected = False
    for _ in range(40):
        check = subprocess.run(command, capture_output=True, text=True, timeout=15)
        if check.returncode == 0 and check.stdout.startswith('8.4.11'):
            connected = True
            break
        time.sleep(2)
    need(connected, '开发工具链沙箱无法使用真实MySQL隔离测试库')
    integration_env = private / 'integration.env'
    integration_env.write_text('SPRING_DATASOURCE_URL=jdbc:mysql://host.docker.internal:' + str(database_port)
                               + '/rbac_it\nSPRING_DATASOURCE_USERNAME=rbac_it\nSPRING_DATASOURCE_PASSWORD='
                               + (private / 'mysql-it').read_text().strip() + '\n')
    integration_env.chmod(0o600)
    record('M7', {'containerName': container_name, 'imageId': image['Id'], 'database': 'rbac_it',
                  'host': 'host.docker.internal', 'port': database_port,
                  'environmentFile': integration_env.relative_to(root).as_posix(),
                  'probe': '与开发相同工具链真实SELECT VERSION(),1通过；Maven verify须注入该环境并执行MySQL集成测试'})

    backups = private / 'backups'
    backups.mkdir(mode=0o700, exist_ok=True)
    backups.chmod(0o700)
    mysql_command = ['docker', 'exec', '-i', container_name, 'sh', '-c',
                     'MYSQL_PWD=$(cat /run/secrets/mysql-root) exec mysql -uroot -N']
    started = time.monotonic()
    try:
        run(mysql_command, data='CREATE DATABASE lf_probe; CREATE DATABASE lf_probe_restore; CREATE TABLE lf_probe.sample(id INT PRIMARY KEY); INSERT INTO lf_probe.sample VALUES(1);')
        dump = run(['docker', 'exec', container_name, 'sh', '-c',
                    'MYSQL_PWD=$(cat /run/secrets/mysql-root) exec mysqldump -uroot --set-gtid-purged=OFF lf_probe'])
        backup = backups / 'capability-probe.sql'
        backup.write_text(dump)
        backup.chmod(0o600)
        run(mysql_command, data='USE lf_probe_restore;\n' + backup.read_text())
        need(run(mysql_command, data='SELECT COUNT(*) FROM lf_probe_restore.sample;') == '1', '备份恢复读回失败')
        backup.unlink()
    finally:
        run(mysql_command, data='DROP DATABASE IF EXISTS lf_probe; DROP DATABASE IF EXISTS lf_probe_restore;')
    record('D1', {'backupDirectory': backups.relative_to(root).as_posix(), 'mode': '0700',
                  'restoreProbeSeconds': round(time.monotonic() - started, 2),
                  'probe': '真实MySQL独立探针库导出、落盘、恢复、读回并清理；业务库实际备份仍由运维执行'})
    need(pending.issubset(set(result['checks']) | set(result['deferred'])), '仍有未闭合执行条件')
    result['status'] = 'passed'
    result['observedAt'] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    result['instructions'] = [
        '批准方案的missing保留为历史设计待办，以本独立执行预检闭合M1-M7和D1；Q1-Q3在部署和QA实际核验。',
        '开发在进程环境读取private/integration.env后执行批准的mvn verify；不得读取后回显或把凭据提交、归档。',
        '部署使用secretRefs创建的文件；开发只提交secrets路径配置和变量名，不提交文件内容。',
        'QA从admin-password文件读取管理员口令；日志、截图和报告不得含口令。',
        '运维在merge-ledger/state.json先记录合并意图，再记录真实合并SHA；重试先核对远端。',
        '原生Git命令可用private/git-askpass.py及AGENTFLOW_GIT_TOKEN作进程级认证；禁止修改全局Git配置或把令牌写入remote。',
        '测试库只供本flow集成测试；业务库由批准的Compose创建；部署前重查端口和权限。',
        '交付时保留本flow正在使用的环境，提供精确停止和清理命令，不执行全局prune。']
    save(directory / 'environment-ready.json', result)
    return result
