---
name: bmad-product-brief
description: Use when creating, updating or validating a product brief, clarifying a product idea, or defining users, scope, success criteria and assumptions before technical planning. Based on BMAD V6.12's standalone product-brief protocol.
---

# 产品简报与需求定型

先读取本技能目录中的 `references/product-brief-protocol.md`，使用其中的
Create、Update、Validate 方法。它是 BMAD V6.12 提供的独立产品简报协议；
本入口明确其在 LoopForge 中的交互和产物位置，不依赖 `_bmad/` 或 `uv`。

## LoopForge 中的执行约定

- 保持调用本技能的角色身份和用户指定的语言，不切换为上游示例中的虚构人物，
  不强制使用图标前缀。`references/original-instructions.md` 只保留原始来源；
  其中创建 Gem、Custom GPT 或配置知识库的安装步骤不属于本任务。
- 上游的 Canvas 对应本次任务工作区中的产品简报文件与补充材料。
  有约定路径时使用约定路径；否则按任务确定一个明确的 Markdown 交付物路径。
  只读验证模式默认返回报告，不修改已有简报。LoopForge 会话回复和正式产物
  按平台现有方式保存，不声称它们会在会话结束时丢失。
- 先读取用户已提供的材料与已有简报，只澄清影响产品判断的实际缺口。
  需要快速推进时采用协议中的 Fast path，集中列出关键问题和显式假设；
  信息足够时直接形成草稿，不重复提问。需要用户作出的决定交回当前会话或
  平台关口，不另外建立一套审批、派工或持续等待机制。
- 技术选型、代码实现、分支与部署由相应角色和平台任务处理。本技能专注
  产品问题、用户、价值、范围、优先级、成功标准及假设，避免越界代替技术方案。
- 时效性事实需要可核对来源。没有可用搜索或证据时明确标为待验证，
  不编造市场数据、用户反馈、竞争优势或验收结果。

## 交付要求

按任务目的和风险控制篇幅。简报应能说明：要解决的问题、目标用户与真实场景、
方案与价值、可衡量的成功标准、首期范围及明确排除项。假设、待确认问题、
已作决策和支撑材料放在补充部分，标明它们如何影响后续设计与研发。

创建模式产出草稿，更新模式核对变更信号并调整已有简报，验证模式输出有位置、
影响与依据的发现。产品简报定型后交接技术规划与设计，不能将简报通过等同于
代码实现、测试完成或上线验收。

来源：BMAD-METHOD V6.12，commit
`bab9082c3e884e0d72c759bcd1a6d90bcc01be84`。原始协议及 MIT 许可随技能保留；
本文件是 LoopForge 的能力资产入口，不是 BMAD 官方运行时。
