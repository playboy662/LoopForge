# 阶段报告的固定字段

本文件规定现有交付脚本实际接收的字段。适用于共享交接 Skill 的 v2、v4 阶段，不包含项目地址、凭据或特定业务要求。路径始终取当前节点的 artifactOutputs。

## 通用要求

- 每个阶段先读取本次 flow.json；方案阶段还须读取 approved-product 的 productDigest。不得重新生成身份。
- JSON 顶层必须是对象。字段名和类型不得自行替换；说明性扩展字段不能代替下列字段。
- `ready` 表示该阶段资料可审阅。尚未核验的执行条件逐项写入 `missing`，不能写成已完成。
- 完成报告后，用本 Skill 的 `scripts/check-report.py <阶段> <实际JSON路径>` 检查。该检查只核对结构，不代替真实浏览器、数据库、构建或权限验证；通过后才能提交本节点的 pass。
- 不得修改上游归档、自动校验脚本或批准文件来消除失败。字段不合规时修正自己的报告。

## product

```json
{
  "flowId": "从本次 flow.json 原样读取",
  "status": "ready",
  "openQuestions": [],
  "acceptance": [{"id": "A1", "description": "可操作、可观察、可判定的验收标准"}]
}
```

每个验收 ID 唯一。存在真实业务歧义时先请求补充信息，不得虚构用户回答。

## ui

```json
{
  "flowId": "继承本次编号",
  "status": "ready",
  "uiRequired": true,
  "acceptanceIds": ["A1"],
  "interactionEvidence": [{"acceptanceId": "A1", "action": "真实执行的交互", "result": "真实观察结果"}]
}
```

有 UI 时，acceptanceIds 覆盖产品验收 ID，交付自包含 HTML 原型及真实浏览器交互证据；每条证据引用有效验收 ID。无 UI 时使用 `uiRequired:false`、非空字符串 `reason`、空数组 `interactionEvidence`，不制造原型。

## technical

技术方案正文必须按同 Skill 的 `references/technology-stack.md` 单列「技术栈与选型依据」，分别说明前端、后端、数据库、测试和交付组件的确定版本及选择依据。此要求补充正文，不替换以下现有 JSON 字段。

先完整读取同 Skill 的 `examples/multi-repository.json`，按实际环境逐字段填写。至少包含：

| 位置 | 固定字段 |
| --- | --- |
| 顶层 | flowId、productDigest、status、productDocs、changes、apiContracts、missing、repositories、services、deployables、deliveryPolicy |
| repositories[] | id、repoPath、access、provider、baseSha；变更仓库另有 workBranch、deliveryBranch、deliveryPermissionEvidence |
| services[] | id、repoId、path、kind、changeRequired、deployableIds；变更服务另有 buildCommand、testCommand |
| deployables[] | id、serviceIds、action、testUrl、credentialRefs、verification；按 action 和批准的交付方式填写仓库、版本及执行字段。Docker Compose 使用 docker-compose-delivery.md 的字段，CI Job 使用示例中的 CI 字段 |
| deliveryPolicy | environment、mergeAuthority、autoIntegrateDisabled、mergeAuthorityEvidence、maxReworks、isolation |

代码交付例外：当本轮明确只做代码交付时，技术报告顶层和 `deliveryPolicy` 都必须写 `scope:"code_only"`，`maxReworks` 仍为 3；`deployables` 必须为空，所有 `services[].deployableIds` 必须为空。此模式不要求 environment、mergeAuthority、autoIntegrateDisabled、mergeAuthorityEvidence 或 isolation，也不得为了通过门禁虚构部署证据。仓库读写权限、基线、分支、接口、构建/测试命令和其他实际环境缺项仍须照常核验并记录。

服务定义的标识字段是 **`services[].id`**；用例或部署单元引用它时使用 `serviceIds`。`serviceId` 不能替代服务定义中的 id。仓库路径为 repoPath，服务相对路径为 path；不得替换为 directory。交付策略字段为 deliveryPolicy，不使用 deliveryStrategy。buildCommand、testCommand 是字符串，不是数组。

服务与部署单元的映射必须双向一致。源代码尚不存在的初始化项目，可在已绑定仓库根目录建模 `path:"."`，方案阶段不创建业务代码目录。实际仓库 ID、目录、HEAD、工作分支从运行绑定及 Git 查询读取；示例的名称、地址和 SHA 不能复制为真实值。

独立方案允许环境待办，但字段和类型仍须完整：未知 URL 用空字符串，未知凭据引用用空数组，缺项写入 missing；未知证据不能用“已确认”代替。全流程在编码前另有严格的环境与权限检查，missing 非空时不能进入实际交付。

## database

需要变更时：

```json
{
  "flowId": "本次编号",
  "productDigest": "批准产品摘要",
  "status": "ready",
  "changesRequired": true,
  "engine": "方案选定的数据库引擎",
  "version": "方案选定的目标版本",
  "design": "设计正文的简明说明及 database-design.md 引用",
  "rollback": "迁移失败或回退时的具体处理及适用限制",
  "missing": []
}
```

engine、version、design、rollback 均为非空字符串，不能使用嵌套对象代替。详细表设计可放在其他字段，不能替代 design。目标引擎/版本属于设计选择，不等于已核验运行环境；运行数据库的目标或实际执行条件未确认时必须在 missing 登记。通过批准的 Docker Compose 新建测试库时，依照 docker-compose-delivery.md 区分方案阶段的条件确认与部署阶段的实际运行核验，不虚报未来数据库已经运行。除 JSON 外，还须交付 database-design.md 和 database.sql。DDL 遵循项目和已挂载数据库技能的正式约束。

无变更时，changesRequired 为 false，提供非空字符串 reason 及 database-design.md，不能为了过门禁造 SQL。

## testplan

```json
{
  "flowId": "本次编号",
  "productDigest": "批准产品摘要",
  "status": "ready",
  "requiredCaseIds": ["T1"],
  "cases": [{"id": "T1", "acceptanceId": "A1", "serviceIds": ["实际 services[].id"], "steps": ["明确操作"], "expected": "可判定的预期结果"}]
}
```

id 唯一，requiredCaseIds 引用真实用例，acceptanceId 引用批准产品的验收项，serviceIds 引用本轮 technical.services[].id。必测用例共同覆盖全部产品验收项和相关服务。根据方案安排真实 API、前端、浏览器交互、授权拒绝及适用回归；本阶段只交付计划，不能宣称测试已执行。

## 后续阶段

开发、独立审核、测试部署和 QA 继续严格采用 references/stages.md 的对应章节及批准的 base/current/operations-context；不得把本文件的方案 status=ready 用于开发或 QA。各阶段 flowId、baseId、planDigest、roundId 必须属于本次冻结方案及研发轮次。业务缺陷进入返工，权限/环境问题暂停，不能改测试标准或伪造通过。

### Git 开发报告固定结构

本节仅用于 Git/v2/v4 交付；`deliveryMode=workspace_v1` 继续使用 workspace-delivery.md。

`draft/development.json` 的 `commits` 必须为对象，键恰好等于批准方案中 `repositories[].access=change` 的仓库 `id`，值为实际完整的 40 位小写 Git SHA。不要输出 `{repoId, branch, sha}` 数组。`checks` 每个变更服务分别记录 build 和 test 两项；每项使用 `serviceId`、`phase`、`command`、`exitCode`、`log`，不能把两次命令折叠成 `buildCommand/testCommand/buildExitCode/testExitCode`。

```json
{
  "flowId": "继承 flow.json",
  "baseId": "继承 base.json",
  "planDigest": "继承批准方案 SHA256",
  "roundId": "继承 current.json",
  "status": "passed",
  "scopeChangeRequired": false,
  "commits": {"批准仓库 id": "实际完整 Git SHA"},
  "checks": [
    {"serviceId": "批准服务 id", "phase": "build", "command": "批准 buildCommand 原文", "exitCode": 0, "log": "真实构建证据"},
    {"serviceId": "批准服务 id", "phase": "test", "command": "批准 testCommand 原文", "exitCode": 0, "log": "真实测试证据"}
  ]
}
```

报告完成后执行 `scripts/check-report.py development <报告路径>`。此检查只核对结构；仓库集合、实际 HEAD、基线祖先、工作分支、批准产品文档内容及命令覆盖仍由流程门禁实查。已有真实证据只需修正字段时，保留提交、身份和日志事实，不能伪造新测试或改批准命令。
