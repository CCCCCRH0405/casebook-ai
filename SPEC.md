# Casebook — Compliance Case Management
## 产品规格书 v0.1 (2026-06-12)

> 工作名 **Casebook**(候选,最终名可改;repo: `compliance-case-management`)。
> 本文档同时是开发蓝图和未来 README 的母本。所有 UI 文案、字段名、状态名以本文英文为准。
> ⚠️ Clean-room 原则:本项目不复用旧 portal 的任何代码、命名、配色、布局。旧物只作流程参考。

---

## 0. 一句话

> **A local case tracker for small compliance teams. Runs from a folder — no server, no cloud, no IT ticket.**

故事框架(README/Reddit 用,已定稿方向):
> I work in compliance. Outside the daily routine, the real pain is keeping track of everything else — inquiries, filings, reviews, remediation items, recurring obligations, "where are we on this?" questions. I built this for myself because spreadsheets kept falling apart under that load. I probably can't even use it at my own firm (IT only gets stricter every year), so I'm open-sourcing it for anyone whose team still runs on Excel.

**是:** 3–10 人合规团队的工作追踪(谁负责/谁代班/何时到期/做过什么留痕/一键导出 Excel 应对监管问询)。
**不是:** GRC 平台;不替代企业系统;不含监管内容或合规建议;不上云——数据永远不离开用户选定的文件夹。

---

## 1. 北极星场景(从合规人视角,"最牛逼"的判据)

每个版本发布前用这 7 个场景验收,任何一条变慢/变难都算回归:

| # | 场景 | 验收标准 |
|---|------|---------|
| N1 | 经理问 "Where are we on X?" | 搜索→打开 case→时间线,**5 秒内答得上来** |
| N2 | 周期性义务永不漏 | 年审/季报到期前自动生成 case 并出现在 Today |
| N3 | "我下周休假" | 一个 coverage request,批准后全程留痕,代班人自动获得编辑权 |
| N4 | 监管/审计来要记录 | 一键 **Examiner Package**:按日期范围导出 case 清单+完整历史 xlsx |
| N5 | 临时来活儿(电话/邮件/路过) | Quick-add 一行字回车,**10 秒内记录在案**,稍后再补细节 |
| N6 | "什么东西卡在谁手里?" | Waiting-on 报表:按阻塞方分组的账龄(如 "Legal 手里压了 3 周的有哪些") |
| N7 | "去年这事谁做的?文件在哪?" | Obligation 页直接列出历年 case 和归档路径 |

N6 是差异化关键:把"卡在哪"做成结构化字段而不是埋在笔记里——合规人最需要、Excel 最做不到的就是这个。

---

## 2. 部署形态

- **单个可执行文件**(Windows .exe / macOS app),无安装器、无依赖、无管理员权限。
- **Host 模式**:组里一个人双击运行 = host;程序给出局域网链接(`http://<host>:8484`),其他成员浏览器直接用,零安装。
- 数据 = host 机器上用户选定的**工作文件夹**:

```
My Compliance Casebook/        ← 首次运行时用户选定
├─ casebook.exe                ← 双击这个(macOS: Casebook.app)
├─ workspace.cbk               ← 活数据(SQLite 单文件;备份 = 复制它)
├─ Records/                    ← 自动维护的 Excel 台账副本(只读快照)
│   ├─ Cases_2026-06.xlsx
│   └─ ActivityLog_2026-06.xlsx
├─ Backups/                    ← 每日自动 zip(保留最近 14 份)
└─ Imports/                    ← 用户放模版填好的导入文件
```

- 活数据文件**建议放本机磁盘**;可在 Settings 里配置 "mirror Records/ & Backups/ to a second folder"(如共享盘),满足"组里都看得到台账"的习惯。
- Host 关机 → 成员浏览器显示明确的 "Host is offline" 页(写清找谁),不静默坏。
- 信任模型:办公室内网、成员下拉选身份 + 可选 4 位 PIN。与"共享盘上的 Excel"同级信任,README 写明;真正的登录认证 = 划给企业产品的界线。

技术栈(定):**Go 后端**(单静态二进制,跨平台交叉编译,embed 前端资源)+ **SQLite**(WAL 模式,纯 Go 驱动免 cgo)+ **原生 HTML/CSS/JS 前端**(无框架,保持可审计、零构建依赖)。

---

## 3. 数据模型

### 3.1 Case(核心工作项)
| 字段 | 类型 | 说明 |
|------|------|------|
| `case_no` | 文本 | 自动编号 `CB-2026-0001`,全局唯一,永不复用 |
| `title` | 文本 | 必填,唯一的必填项(Quick-add 只要这个) |
| `case_type` | 引用 | 见 3.2 |
| `status` | 枚举 | 见 §4 状态机 |
| `owner` | 成员 | 可为 Unassigned(进 pool) |
| `due_date` | 日期 | 下一步行动期限(驱动 Today/提醒) |
| `deadline` | 日期 | 硬截止(监管期限),与 due_date 分开 |
| `waiting_on` | 枚举+文本 | None / Business Unit / Legal / IT / Regulator / Vendor / Other(自由文本);配 `waiting_since` 自动记账龄 |
| `source` | 文本 | 谁/哪来的(邮件、电话、考试函编号) |
| `location` | 文本 | 工作底稿路径/链接(不存文件,只存指针) |
| `tags` | 多值 | 自由标签 |
| `obligation_id` | 引用 | 若由周期义务生成则回链 |
| `completed_at` / `archived_at` | 时间戳 | 完成后 7 天冷却自动归档(可配置) |

### 3.2 Case Type(用户可自定义)
预置:`Inquiry` `Filing` `Review` `Remediation` `Request` `Task`。Settings 里可增删改(颜色+名称)。**不做层级编号体系**——这是与 testing 工具划清界限的有意决定;要分类用 type+tags。

### 3.3 Obligation(周期义务)— 用户自建
| 字段 | 说明 |
|------|------|
| `name` | 如 "Annual CEO Certification" |
| `recurrence` | Yearly / Quarterly / Monthly / Weekly / Every N days,锚定日期 |
| `lead_time_days` | 提前 N 天自动生成 case |
| `default_owner` | 生成的 case 默认给谁(可 Unassigned) |
| `checklist_template` | 生成 case 时自带的清单项 |
| `active` | 可暂停 |

引擎规则:host 启动时及每日 00:05 扫描;`today >= next_due - lead_time` 且该周期尚无 case → 生成,事件记 `SPAWNED`。Obligation 详情页列出**历年所有 case**(N7)。

### 3.4 Event(时间线,append-only)
`event_id, case_no, at, actor, kind, payload(JSON)`
kind:`CREATED / NOTE / STATUS / FIELD_CHANGE / ASSIGNED / REQUEST / DECISION / COVERAGE / SPAWNED / IMPORTED / REOPENED / ARCHIVED`
**只追加,永不 UPDATE/DELETE**。改错了?追加更正事件。这是审计可信度的根。

### 3.5 Request(协作请求)
`request_no, case_no, kind(COVERAGE/TRANSFER/HELP), from, to, message, status(OPEN/ACCEPTED/DECLINED/WITHDRAWN), resolved_by/at`
Accept 的副作用(换 owner、生效 coverage)与状态更新在**同一事务**内完成——旧架构里"副作用成功但状态没改"的缝在 SQLite 事务下天然不存在。

### 3.6 Coverage / Member / Checklist
- Coverage:`case_no, original_owner, covering_member, start, expected_end, status(ACTIVE/ENDED)`;active coverage 给代班人完整编辑权,case 页显著标注。
- Member:`name, initials, color, pin(可选), active`;停用不删除(历史事件引用)。
- Checklist item(case 内):`label, state(Needed/Requested/Received/N.A.), date, note` — 证据收集追踪,轻量版,不存文件。

---

## 4. 状态机

```
            ┌──────────────┐
  create →  │    Open      │ ←──────────── Reopen(记 REOPENED)
            └──┬───────────┘                     ↑
               ↓                                 │
            In Progress ⇄ Waiting(必选 waiting_on)│
               ↓                                 │
            Completed ──(冷却 7 天)──→ Archived ─┘
               │
            Cancelled(任何状态可达,必填原因)
```

- 编辑权:owner、active coverer、(Unassigned 的 case 人人可认领)。其他人:可读、可加 note、可发 request。
- 创建时可直接指派给别人(小团队现实:经理派活),被指派人收到 Inbox 通知事件;**抢**别人的 case 必须走 TRANSFER request。
- Completed→Archived 由引擎自动;Archived 只读,可 Reopen。

---

## 5. 导入 / 导出

### 5.1 批量导入(给已有 Excel 台账的团队)
1. Import 页下载模版 `casebook-import-template.xlsx`(列头+下拉+示例行+说明 sheet);
2. 填好放回上传(或丢进 `Imports/`);
3. **校验报告**:逐行错误(第 12 行:无效日期 "TBD")→ 全对才允许进入预览;
4. 预览 diff → 确认 → 提交,每行记 `IMPORTED` 事件。
Obligations 同样有独立模版。**绝不静默跳过错误行。**

### 5.2 导出
- **Records/**:每月+每次大变更后自动刷新 xlsx 快照(Cases、ActivityLog),普通成员"随时打开 Excel 看台账"的习惯零成本保留;
- **Examiner Package**(N4):选日期范围/类型/成员 → 一个 xlsx:Summary sheet + Cases + 每 case 完整事件史 + Obligations + Members,文件名含生成时间戳;
- **.ics 日历**:所有 due/deadline 导出为日历文件,Outlook 直接订阅——零基础设施的"提醒系统"。

---

## 6. 首次运行向导(逐屏)

| 屏 | 内容 | 按钮 |
|----|------|------|
| 1 | 欢迎 + "你的数据只存在你选的文件夹里" | Choose Folder… |
| 2 | 系统文件夹选择器 → 显示将创建的结构预览 | Create Workspace |
| 3 | Team name + 你的名字(= 第一个成员 + admin) | Continue |
| 4 | (可选)添加其他成员名单,可跳过 | Continue / Skip |
| 5 | 完成页:大字显示成员访问链接 + "Copy link" + 示例数据按钮 | Open Dashboard |

- 第 5 屏的 **"Load sample data"**:一键生成演示工作区(虚构 case/义务/历史)——Reddit 观众 30 秒内看到活的产品,这是 demo 转化率的关键。
- 以后双击 = 直接启动上次的 workspace(可在 Settings 换)。

---

## 7. UI 设计语言(clean-room,与旧 portal 切割)

**审美方向:正式 SaaS,白、净、排版驱动** — 受众是 Excel 原住民,要的是密度和秩序感,不是炫。
**反格子原则(用户明确要求)**:杜绝"AI 生成感"的卡片格子堆叠——统计数字不做四宫格 metric card,改为一行内联数字+细分割线;面板尽量用留白和 hairline 分割线组织,而不是一个个圆角盒子;badge/chip 克制使用。参照系:Linear / Stripe Dashboard 的"排版而不是盒子"。

| 维度 | 决定 | 与旧 portal 的切割 |
|------|------|-------------------|
| 布局 | **顶部导航条**,无侧边栏 | 旧版是左侧 sidebar |
| 底色 | **纯白 `#FFFFFF`**,区域用细线分割,不用底色块 | 旧版米灰底+白卡片 |
| 主色 | **Ledger Green 墨绿 `#1B5E4A`**,克制使用 | 旧版 indigo `#4f46e5`,彻底告别 |
| 字体 | 系统 UI 字体;数字/编号 tabular-nums | — |
| 表格 | 表格优先(默认视图),hairline 行线,无斑马纹 | 旧版卡片优先 |
| 状态色 | 状态=灰阶文字+语义色小圆点;只有 overdue 用红 | 旧版彩色 chip 满天飞 |
| 交互核心 | **Quick-add bar 常驻顶部**(N5),`/` 聚焦 | 旧版无此概念 |

主要页面:**Today**(默认页:我的到期+逾期+Inbox+本周日历条)/ **Cases**(全量表格+保存的筛选)/ **Case 详情**(左:字段+checklist;右:时间线+快速记事框)/ **Calendar & Obligations** / **Inbox**(requests)/ **Reports**(Waiting-on 账龄、workload、完成统计)/ **Import** / **Settings**(members/types/folders)。

所有写操作:乐观 UI + 即时事件落库(本地 SQLite 无队列延迟,旧版"Saving…"焦虑不复存在);任何失败必须弹出可读错误,**无静默失败**。

---

## 8. 质量策略("每一个按钮都真的 work")

1. **引擎单元测试**:状态机全转移矩阵(合法/非法)、周期引擎(闰年/月末/lead time 边界)、导入校验(每种错误类型一个用例)、编号器并发。
2. **API 测试**:每个端点的成功/权限拒绝/坏输入三件套。
3. **E2E(Playwright)**:对照"按钮清单"逐一驱动真浏览器——清单 = 每个页面每个可点元素 × (正常路径 + 取消路径 + 空数据状态)。新按钮没进清单不许合并。
4. **多用户模拟**:两个浏览器 context 并发 claim 同一 case / 同时 resolve 同一 request → 断言只成功一个且无双写。
5. **数据安全**:schema 版本号+自动迁移;每日备份;打开旧版本文件自动升级并先备份。
6. **发布前手工五分钟**:向导走一遍 + 北极星 N1–N7 过一遍,Windows 和 macOS 各一次。

---

## 9. 里程碑

| 版本 | 内容 | 验收 |
|------|------|------|
| M0 骨架 | 二进制+向导+members+case CRUD+时间线+Cases 表格 | N1, N5 |
| M1 协作 | requests(coverage/transfer/help)+coverage 权限+归档+Today 页 | N3 |
| M2 周期 | obligations+引擎+Calendar+.ics | N2, N7 |
| M3 进出 | 导入模版+校验+Examiner Package+Records/ 快照 | N4, N6(报表) |
| M4 发布 | E2E 全清单+sample data+README+demo GIF+Win/mac 产物 | 全部 N |

---

## 10. 待用户拍板(不阻塞 M0)

1. 产品名:**Casebook** vs 纯描述名 "Compliance Case Manager" vs 其他;
2. 默认语言:UI 英文(受众主要在美国合规圈)——默认按英文做;
3. Reports 页给经理看的口径(等 M3 前再定)。
