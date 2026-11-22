# 无 Git 的工作区交付协议

只在项目明确设置 deliveryMode=workspace_v1 时使用。本协议覆盖交接适配 v2/v4 的 Git、仓库、提交、PR/MR 和 CI 字段；不创建 Git，不推送、不合并。共享角色职责继续有效。其他项目继续原协议。

所有正文、JSON和演示原型交到运行根目录 `.loopforge-workspace/`，实际应用只在 `app/`。不要改平台提供的 `.agentflow/inputs`、本协议检查器、nodes.json、flow.json或别人的报告。上游以本次平台归档为准，路径从节点上下文获取。通过 file SHA256 与 sourceDigest 锁定实际交付文件，不把 sourceDigest 写成 Git SHA。

每个角色先读 flow.json、相关上游归档和当前节点名称。所有报告顶层均含 flowId。产品、UI、技术、DB、测试计划使用 status=ready；技术、DB与测试计划还继承产品包的 productDigest，均提供 missing 数组。JSON字段不得任意改名。证据必须实际执行；缺工具、权限、环境时提交 human，不虚构passed。最终仍需 submit_gate_result。

若存在 `.loopforge-workspace/ledger-mode.json` 且 `profile=generic_ledger_v1`，同时读取本 Skill 的 `references/ledger-generation.md`，按当前角色完成台账建模、生成或专项验证。此标记由通用台账流程初始化，其他流程不启用该扩展。

技术方案还须读取同 Skill 的 `references/technology-stack.md`，在 technical-plan.md 单列「技术栈与选型依据」，分别说明实际前端、后端、数据、测试及交付组件与版本。原生页面或 Node 原生 HTTP 也要明确说明；不因通用工具链安装了 Java 或某份技能使用 Vue 就替换已批准选型。

| 当前节点名称 | 负责的交付 |
|---|---|
| 产品定义 | product.md、product.json：status=ready、openQuestions=[]、acceptance数组{id,description}；明确范围、用户路径、异常和验收项 |
| 交互与原型 | ui-design.md、ui.json、适用prototype.html：uiRequired布尔值；适用时interactionEvidence数组{acceptanceId,action,result}覆盖全部验收，实际Chromium操作；不适用给reason |
| 技术与接口方案 | technical-plan.md、technical.json：appRoot=app、commands={build,test}（可独立运行的字符串命令）、apiContracts数组、deployment见下文；使用当前工具链与可用稳定依赖，解释选择，避免不必要的服务数量 |
| 数据库设计 | database-design.md、database.json，适用database.sql：changesRequired布尔值；适用时engine/version/design/rollback字符串，写清实际引擎版本和隔离方式；不适用给reason。所有DDL含表/字段注释、not null及合理默认值。仅设计，不提前执行DDL |
| 测试计划 | test-plan.md、test-plan.json：cases数组{id,acceptanceId,steps,expected}及requiredCaseIds；必测覆盖全部产品验收，涵盖API、UI、边界和错误反馈 |
| 全栈实现与自测 | development.md、development.json，实际代码在app/：继承current.json的flowId/planDigest/roundId，status=passed，记录实际构建测试。按批准方案交付Compose、Dockerfile、健康/版本接口、适用迁移。只做实现，不部署。feedback有缺陷时优先修复，不能改验收标准 |
| 架构与安全审核 | code-review.md、code-review.json，独立只读检查app/，引用release.json的sourceDigest |
| 数据库专项审核 | database-review.md、database-review.json，同上，检查批准设计、实际DDL和查询/事务；不适用提供实际原因 |
| UI交互审核 | ui-review.md、ui-review.json，同上，核对批准产品、原型、实际实现和可访问性；不部署、不修改实现 |
| Docker测试交付 | deployment.md、deployment.json：实际执行部署，字段与版本见下文。使用共享原生Docker沙箱；不改业务代码 |
| 独立QA验收 | qa.md、qa.json：status=completed、quality=passed或failed，cases数组{id,status:passed或failed,evidence}，defects数组；引用本轮身份和sourceDigest。实际浏览器、真实API和适用数据库验证，不能用产品原型代替应用 |

产品完成后执行正式产品人审，方案完整后执行正式方案人审。未经方案批准不写应用。审核与QA发现业务缺陷回到研发轮次，最多三轮；环境故障转人工处理。稳定缺陷ID、location、description、fixHint必须明确，不能用重写标准消除缺陷。

技术方案 deployment 固定：provider=docker_compose、composeFile（如app/compose.yaml）、service、hostPort整数、testUrl、healthUrl、versionUrl、environmentEvidence字符串。端口先查占用，部署前重查，不停止别人的服务。Docker沙箱不能访问宿主socket，方案可引用项目环境中已验证的daemon事实，部署节点再实际核验。数据库、数据卷、网络均按flow隔离；没有真实账号、报价或运单系统时使用明确标注的演示数据，不发真实业务短信、订单或预约。

运行模式：主流程和开发/审核/QA使用共享Java/Node Docker工具链；运维子流程使用原生Compose沙箱，共享同一工作区。应用进程在独立Compose容器中运行，禁止依靠开发沙箱中的后台进程冒充部署。跨沙箱测试URL使用实际可达的host.docker.internal入口，用户本机入口另行记录。

开发门禁会重新执行批准的build/test命令，计算app/源文件清单及sourceDigest。审核报告均含flowId/planDigest/roundId/sourceDigest、status=passed或rework、findings数组、evidence字符串。passed时findings为空；rework时每条缺陷至少有id/location/description/fixHint。审核不得改app/；门禁会重新计算哈希核对。

运维读取release.json和三项审核归档，只从审核通过的app/构建。Compose项目名固定`lf-<完整flowId>`；容器标签含`io.loopforge.flow-id`、`io.loopforge.round-id`、`io.loopforge.source-digest`，值分别来自本轮身份和release.sourceDigest。Compose的service标签须与批准方案一致，应用须有实际Docker HEALTHCHECK。不要设置全局container_name，不挂平台目录和用户home，不挂宿主socket。

deployment.json含flowId/planDigest/roundId/sourceDigest、status=passed、containerId（完整64位）、imageId（sha256:镜像ID）、testUrl、userUrl、executionEvidence、healthEvidence、versionEvidence。应用版本接口返回sourceDigest；构建时传入此值，禁止硬编码虚构版本。部署后检查器实际读取Docker daemon核对容器运行、healthy状态、镜像和标签，查询失败即停止。QA再次核对实际HTTP版本及报告，记录逐项真实证据。

应用、数据及证据按运行隔离。只回收当前flow的临时资源，不执行全局prune。最终交付包含访问入口、演示方式、已知限制和精确回收命令；源码、报告通过平台产物归档，无须Git。
