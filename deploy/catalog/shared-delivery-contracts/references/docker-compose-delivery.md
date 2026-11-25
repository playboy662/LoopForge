# Docker Compose 测试交付

仅在项目明确选择 Docker Compose 交付时采用本节；CI 项目通过已配置的 MCP 工具调用外部流水线。选择由技术方案的 `deliveryPolicy.provider` 固定为 `docker_compose`，方案审批后不得自行改换方式。

## 环境资产与阶段职责

- 产品、方案、开发、审核和 QA 通过当前 SOP 绑定的 Docker 工具链沙箱执行，使用平台自带 Chromium。原生 Docker 交付沙箱只给运维和需要查询 Docker 的流程脚本。
- Dockerfile 定义工具链，安装脚本只在隔离 PREFIX 安装命令，沙箱引用相应资产。不能把 Docker 安装脚本写成修改宿主配置的脚本。
- 服务器/执行机、网络与端口属于项目环境配置，凭据只用当前实际机制；共享资产不保存服务器密码和模型 Key。
- 技术方案为 Docker 服务确定 API、前端、数据库及必要组件。开发提交应用 Dockerfile、Compose 定义、健康/版本接口与功能代码，不能到运维阶段临时写业务代码。
- DBA 设计数据库迁移与回滚，使用独立数据库容器/数据卷。测试数据和密码运行时生成或从现有安全机制注入，不提交凭据。

新测试库由后续批准的 Compose 定义创建。方案阶段固定数据库镜像和版本、隔离资源、凭据生成与注入方式、可达地址及迁移/回滚责任，并核对执行机的实际 Docker 能力与镜像可用性；明确记录数据库尚未创建，不将未来服务写成已运行。上述执行条件确认后，尚未进行部署本身不属于配置缺项；未知的目标、权限、网络或凭据机制仍必须进入 missing 并暂停。真实数据库版本、健康、连接和迁移结果在部署阶段核验。方案阶段不执行业务 DDL。

## 技术方案字段

延续 repositories、services、deployables、deliveryPolicy、productDocs 等固定字段。deliveryPolicy.provider 为 docker_compose；isolation 使用每 flowId 独立的 Compose 项目、网络、数据库与命名卷，或真实外部锁。

每个 action 非 regression 的 deployable 增加：

```json
{
  "id": "真实部署单元ID",
  "repoId": "真实源码仓库ID",
  "serviceIds": ["真实服务ID"],
  "action": "changed",
  "testUrl": "按项目环境规划的实际测试地址",
  "credentialRefs": [],
  "verification": {"method": "HTTP健康及版本接口并核对Docker inspect", "healthUrl": "实际健康地址", "versionUrl": "实际版本地址"},
  "docker": {
    "composeRepoId": "保存Compose定义的已绑定可写仓库ID",
    "composeFile": "相对该仓库的Compose文件路径",
    "service": "Compose服务名"
  }
}
```

jobUrl、jobParameters、buildUrl 不适用于 Docker Compose，不伪造 CI 地址。Compose 在开发阶段交付，方案阶段不要求该未来文件已经存在。用户配置允许的端口、执行机的 daemon 可达性、构建网络和隔离方式必须有实际证据；未核验项进入 missing。运行前通过批准方案核对端口占用，不能停止别人的容器来腾位置。

Compose project 名固定为 `lf-<完整flowId>`，允许通过 COMPOSE_PROJECT_NAME 注入；同一 flow 的返工继续查询同名项目。每个业务容器和镜像均记录标签：`io.loopforge.flow-id`、`io.loopforge.round-id`，变更单元另有 `org.opencontainers.image.revision`，值为实际构建的完整合并 SHA。前端静态产物 version.json 和后端版本接口与标签一致。

## 运维交付

继续先准备本轮 Git PR/MR，mr-check 成功后，在部署节点核对实际合并状态及全部仓库版本，保留持久的合并记录；部分合并/状态不明立即停止。按已批准的交付方式执行，不自行切换部署入口。

在实际原生交付沙箱中执行 loopforge-docker-check，使用 Docker Compose 从精确合并 SHA 构建。多仓库并排，Compose 构建上下文只指向已批准仓库目录。以 flowId 隔离网络、容器、命名卷和数据库用户，不能使用全局 container_name 或宿主共享数据库冒充隔离。仅发布批准的本机/测试端口，记录从 QA 沙箱可达的地址。

部署报告沿用 flowId/baseId/planDigest/roundId/contextId、status、environment、mergedMRs、partialMerge、mergeJournalEvidence、isolationEvidence、versionCombination。每个非 regression 单元的报告增加：

```json
{
  "deployableId": "与批准单元一致",
  "action": "changed",
  "testUrl": "与批准地址一致",
  "version": "sha256:真实Docker镜像ID",
  "versionEvidence": "脱敏Docker查询和HTTP版本查询结果",
  "healthEvidence": "真实健康检查结果",
  "sourceSha": "审核通过的源码SHA",
  "mergeSha": "实际合并SHA",
  "builtSha": "完整构建SHA",
  "deployedSha": "与builtSha一致",
  "ancestryEvidence": "源提交属于合并构建提交的Git查询",
  "executionEvidence": "构建、启动、健康检查的脱敏日志及时间",
  "docker": {"projectName": "lf-完整flowId", "service": "批准的Compose服务名", "containerId": "实际容器ID", "imageId": "sha256:实际镜像ID"}
}
```

version 与 docker.imageId 必须相等。容器必须运行中、健康检查为 healthy，项目标签、轮次标签和源码标签必须属于本次批准内容；部署后不可仅凭报告中的 passed 判断成功，流程脚本会再次读取 Docker daemon 核对。

## QA 与保留

QA 在真实浏览器验证前后端登录、权限差异、菜单/按钮及直接接口越权拒绝，核对 HTTP 实际版本与 operations.json 完整版本组合。数据库验证实际使用独立测试库。不能用产品演示原型或 Mock 替代业务验收。

业务缺陷按原有稳定 Bug ID 返回开发，最多三轮；环境或 Docker 故障暂停处理。验收完成可按用户授权保留应用供查看，并记录 Compose 项目、入口、数据保留和回收命令。只回收本 flow 的资源，禁止全局 docker prune、删除他人的镜像、卷或停止无关服务。
