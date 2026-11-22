"""Check canonical report shapes before a role hands them to the workflow gate."""
import argparse
import json
import re
import uuid
from pathlib import Path


def require(condition, message):
    if not condition:
        raise ValueError(message)


def text(value):
    return isinstance(value, str) and bool(value.strip())


def rows(value, label, allow_empty=False):
    require(isinstance(value, list) and (allow_empty or value), label + ' 必须为数组')
    require(all(isinstance(item, dict) for item in value), label + ' 的元素必须为对象')
    return value


def unique_ids(items, label):
    identifiers = [item.get('id') for item in items]
    require(all(text(value) for value in identifiers), label + ' 必须使用非空字符串 id')
    require(len(identifiers) == len(set(identifiers)), label + ' 的 id 重复')
    return set(identifiers)


def strings(value, label, allow_empty=False):
    require(isinstance(value, list) and (allow_empty or value) and all(text(item) for item in value), label + ' 必须为字符串数组')
    return value


def check(stage, report):
    require(isinstance(report, dict), '报告顶层必须为对象')
    require(text(report.get('flowId')), 'flowId 缺失')
    uuid.UUID(report['flowId'])
    if stage == 'development':
        require(report.get('status') == 'passed' and report.get('scopeChangeRequired') is False, '开发报告须为 passed 且不变更方案范围')
        for key in ('baseId', 'roundId'):
            require(text(report.get(key)), key + ' 缺失')
            uuid.UUID(report[key])
        require(isinstance(report.get('planDigest'), str) and re.fullmatch('[0-9a-f]{64}', report['planDigest']) is not None, 'planDigest 必须继承批准方案的 SHA256')
        commits = report.get('commits')
        require(isinstance(commits, dict) and commits and all(text(key) and isinstance(value, str) and re.fullmatch('[0-9a-f]{40}', value) is not None for key, value in commits.items()), 'commits 必须为仓库 id 到完整 Git SHA 的对象，不能使用数组')
        checks = rows(report.get('checks'), 'checks')
        seen = set()
        for item in checks:
            require(text(item.get('serviceId')) and item.get('phase') in ('build', 'test') and text(item.get('command')) and type(item.get('exitCode')) is int and item['exitCode'] == 0 and text(item.get('log')), 'checks 每项须有 serviceId/phase/command/整数 exitCode=0/log')
            identity = (item['serviceId'], item['phase'])
            require(identity not in seen, '同服务同阶段检查重复')
            seen.add(identity)
        require(all((service, phase) in seen for service, _ in seen for phase in ('build', 'test')), '每个变更服务须分别提供 build 与 test 检查')
        return
    require(report.get('status') == 'ready', '方案报告 status 必须为 ready')
    if stage in ('technical', 'database', 'testplan'):
        digest = report.get('productDigest')
        require(isinstance(digest, str) and len(digest) == 64 and all(c in '0123456789abcdef' for c in digest), 'productDigest 必须继承批准产品的 SHA256')
    if stage == 'product':
        require(report.get('openQuestions') == [], '产品仍有未确认问题')
        acceptance = rows(report.get('acceptance'), 'acceptance')
        unique_ids(acceptance, 'acceptance')
        require(all(text(item.get('description')) for item in acceptance), '验收项缺 description')
    elif stage == 'ui':
        require(type(report.get('uiRequired')) is bool, 'uiRequired 必须为布尔值')
        if report['uiRequired']:
            identifiers = strings(report.get('acceptanceIds'), 'acceptanceIds')
            require(len(identifiers) == len(set(identifiers)), 'acceptanceIds 重复')
            evidence = rows(report.get('interactionEvidence'), 'interactionEvidence')
            require(all(item.get('acceptanceId') in identifiers and text(item.get('action')) and text(item.get('result')) for item in evidence), '交互证据必须引用验收项并提供 action/result')
        else:
            require(text(report.get('reason')) and report.get('interactionEvidence') == [], '无 UI 时必须提供 reason，且 interactionEvidence 为空数组')
    elif stage == 'technical':
        require(isinstance(report.get('missing'), list), 'missing 必须为数组')
        repositories = rows(report.get('repositories'), 'repositories')
        services = rows(report.get('services'), 'services')
        deployables = rows(report.get('deployables'), 'deployables', allow_empty=True)
        repo_ids = unique_ids(repositories, 'repositories')
        service_ids = unique_ids(services, 'services')
        unit_ids = unique_ids(deployables, 'deployables')
        for item in repositories:
            require(text(item.get('repoPath')) and item.get('access') in ('change', 'read_only') and text(item.get('provider')), '仓库缺 repoPath/access/provider')
            require(text(item.get('baseSha')) and len(item['baseSha']) == 40 and all(c in '0123456789abcdef' for c in item['baseSha']), '仓库 baseSha 必须为实际完整 SHA')
            if item['access'] == 'change':
                require(text(item.get('workBranch')) and text(item.get('deliveryBranch')), '变更仓库缺 workBranch/deliveryBranch')
        for item in services:
            require(type(item.get('changeRequired')) is bool and item.get('kind') in ('frontend', 'backend', 'fullstack', 'worker', 'library', 'shell', 'microfrontend'), '服务缺 changeRequired/kind')
            mapped = strings(item.get('deployableIds'), 'deployableIds', allow_empty=True)
            require(set(mapped).issubset(unit_ids), '服务引用了未定义部署单元')
            if item.get('repoId'):
                require(item['repoId'] in repo_ids and text(item.get('path')), '服务缺有效 repoId/path')
            if item['changeRequired']:
                require(item.get('repoId') in repo_ids and text(item.get('buildCommand')) and text(item.get('testCommand')), '变更服务缺有效仓库或字符串 buildCommand/testCommand')
        for item in deployables:
            mapped = strings(item.get('serviceIds'), '部署单元 serviceIds')
            require(set(mapped).issubset(service_ids) and item.get('action') in ('changed', 'redeploy', 'regression'), '部署单元引用或 action 无效')
            require(set(mapped) == {s['id'] for s in services if item['id'] in s['deployableIds']}, '部署映射不是双向一致')
            require(isinstance(item.get('credentialRefs'), list) and isinstance(item.get('verification'), dict), '部署单元缺 credentialRefs/verification')
        require(isinstance(report.get('apiContracts'), list) and report.get('changes'), '缺 apiContracts/changes')
        docs = report.get('productDocs', {})
        require(isinstance(docs, dict) and docs.get('repoId') in repo_ids and text(docs.get('requirementKey')), 'productDocs 缺有效仓库及编号')
        policy = report.get('deliveryPolicy')
        require(isinstance(policy, dict), '交付策略字段必须为 deliveryPolicy')
        require(policy.get('environment') == 'test' and policy.get('mergeAuthority') == 'sop' and type(policy.get('autoIntegrateDisabled')) is bool and policy.get('maxReworks') == 3 and isinstance(policy.get('isolation'), dict), 'deliveryPolicy 结构无效')
        if policy.get('provider') == 'docker_compose':
            for item in deployables:
                if item['action'] == 'regression':
                    continue
                docker = item.get('docker')
                require(isinstance(docker, dict) and docker.get('composeRepoId') in repo_ids and text(docker.get('service')), 'Docker 单元缺有效 composeRepoId/service')
                compose_file = docker.get('composeFile')
                require(text(compose_file) and not Path(compose_file).is_absolute() and '..' not in Path(compose_file).parts, 'Compose 文件路径必须在批准仓库内')
    elif stage == 'database':
        require(type(report.get('changesRequired')) is bool, 'changesRequired 必须为布尔值')
        if report['changesRequired']:
            require(all(text(report.get(key)) for key in ('engine', 'version', 'design', 'rollback')), 'engine/version/design/rollback 均须为非空字符串')
            require(isinstance(report.get('missing'), list), '数据库 missing 必须为数组')
        else:
            require(text(report.get('reason')), '无数据库变更须说明 reason')
    elif stage == 'testplan':
        cases = rows(report.get('cases'), 'cases')
        identifiers = unique_ids(cases, 'cases')
        required = strings(report.get('requiredCaseIds'), 'requiredCaseIds')
        require(len(required) == len(set(required)) and set(required).issubset(identifiers), '必测用例缺失或重复')
        for item in cases:
            require(text(item.get('acceptanceId')) and text(item.get('expected')), '用例缺 acceptanceId/expected')
            strings(item.get('serviceIds'), '用例 serviceIds')
            strings(item.get('steps'), '用例 steps')
    else:
        raise ValueError('未知阶段：' + stage)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('stage', choices=['product', 'ui', 'technical', 'database', 'testplan', 'development'])
    parser.add_argument('report', type=Path)
    arguments = parser.parse_args()
    try:
        check(arguments.stage, json.loads(arguments.report.read_text(encoding='utf-8')))
    except (ValueError, OSError, TypeError) as error:
        parser.exit(1, str(error) + '\n')
    print(arguments.stage + ' 报告结构通过；真实证据、上游引用与审批由流程继续校验。')
