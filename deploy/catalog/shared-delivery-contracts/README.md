# 共享阶段交接字段修正

2026-11-21：真实方案报告曾使用 services[].serviceId，而现有方案门禁要求 services[].id；数据库报告还存在缺少 engine/version、design/rollback 使用对象而门禁要求字符串的问题。增加固定字段说明和角色交付前检查，保留原有严格门禁。

本目录的 references/report-contracts.md 和 scripts/check-report.py 加入现有共享 Skill loopforge-delivery-handoff-v2，不创建重复技能或业务私有角色。SKILL.md 在 JSON 示例引用前增加必读固定字段说明及交付前检查要求；原有 stages.md、staged-delivery-v4.md 和 multi-repository.json 保留。

技术方案示例中的真实地址、SHA、分支与权限证据仍由当前项目和运行提供。前置检查只确认结构，不能证明模型、浏览器、构建、数据库或部署执行成功。

已验证规范技术报告可接受、serviceId 字段漂移被拒绝，以及数据库四个字符串字段的约束；初次六文件版本已保存并读回核对摘要，后续 Docker 版本通过页面上传完整七文件包。原有 15 项隔离脚本验证通过。真实角色和完整流程的结果另行记录，不能用这些结构检查代替业务验收。

## Docker Compose 交付

新增 references/docker-compose-delivery.md，供选择 Docker Compose 的项目使用。技术方案通过 deliveryPolicy.provider 固定交付方式；SKILL.md 指向该文档，原 stages.md 的 jobUrl/jobParameters/buildUrl 要求限定为 CI Job 交付。原有审批、审核、版本和测试约束保留，不能伪造 CI 地址满足旧字段。

scripts/docker-contract.py 提供流程脚本可嵌入的通用检查：方案中的 Compose 仓库、相对文件和服务名；部署后的实际 Docker 容器、不可变镜像 ID、健康状态、交付身份、轮次和精确合并 SHA。部署检查必须在具备 Docker daemon 访问能力的沙箱执行，通用工具链沙箱不挂宿主 Docker socket。此 helper 属于流程门禁源文件，不要求业务角色修改或安装它。

两个完整门禁与共享交付前检查已完成 11 项隔离检查，覆盖正确方案与部署、路径越界、错误提供方、数据库待办、错误版本标签、不健康容器、镜像漂移、错误构建 SHA 和 daemon 不可达。Git 使用临时真实仓库，Docker 查询使用测试替身；这只能证明门禁行为，不能代替真实业务部署。

另在独立临时真实容器执行两项运行检查：健康容器的实际镜像和标签通过核验，停止容器后被门禁拒绝。探针无网络、端口或数据卷，验证后已删除；仍不代表业务实现及全流程通过。

维护规则：平台或流程更新后，同时核对门禁实际字段、此文档、示例和检查脚本；不得只修改一个样例，让模型与脚本继续使用不同协议。

## Git 开发报告形状修正

真实 WMS 开发报告曾把 commits 写成数组，并把 build/test 检查折叠到同一行；后续严格门禁只接受仓库 ID 到 SHA 的对象以及逐 phase 检查。现为固定报告字段补充 Git 开发完整示例，check-report.py 增加 development 阶段的交付前检查；workspace_v1 继续使用独立协议。五项实证结构检查覆盖旧数组拒绝、规范结构通过、合并检查拒绝、布尔退出码拒绝与缺测试阶段拒绝。通过 Orca 页面完整上传八文件包并保存、刷新确认；其他文件和共享绑定保持。真实业务门禁及审核继续独立执行。

## 技术栈说明

新增 references/technology-stack.md：要求技术方案正文单列前端、后端、数据库、测试及交付组件的确定版本、职责与依据，项目配置保存便于查找的摘要。Git 和 workspace 两种协议共用该说明，不强制统一语言或框架，不新增门禁 JSON 必填字段，也不改已冻结方案。通过 Orca 页面完整上传九文件 Skill 并保存、刷新确认，现有开发报告检查器和其他附件保留。
