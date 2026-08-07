# Skills.sh 热门 Skills 榜单

> 数据来源：[skills.sh/trending](https://skills.sh/trending) All Time 榜单（全站累计 902,139 次安装），截至 2026 年 7 月。
>
> 安装任意 skill：`npx skills add <owner/repo@skill> -g -y`（`-g` 全局安装，`-y` 跳过确认）。

---

## 🏆 全站 Top 10

| 排名 | Skill | 来源 | 安装量 |
| :--: | --- | --- | ---: |
| 1 | `find-skills` | vercel-labs/skills | 17.0K |
| 2 | `grill-mem` | mattpocock/skills | 8.9K |
| 3 | `just-scrape` | scrapegraphai/just-scrape | 8.7K |
| 4 | `grill-with-docs` | mattpocock/skills | 7.8K |
| 5 | `grilling` | mattpocock/skills | 7.3K |
| 6 | `to-issues` | mattpocock/skills | 7.3K |
| 7 | `lark-approval` | open.feishu.cn | 7.2K |
| 8 | `improve-codebase-architecture` | mattpocock/skills | 7.2K |
| 9 | `lark-drive` | open.feishu.cn | 7.2K |
| 10 | `tdd` | mattpocock/skills | 7.2K |

---

## 按类别推荐

### 🎨 前端 / 设计

| Skill | 来源 | 安装量 | 安装命令 |
| --- | --- | ---: | --- |
| `frontend-design` | anthropics/skills | 4.5K | `npx skills add anthropics/skills@frontend-design` |
| `design-taste-frontend` | leonxlnx/taste-skill | 5.2K | `npx skills add leonxlnx/taste-skill@design-taste-frontend` |
| `high-end-visual-design` | leonxlnx/taste-skill | 3.7K | `npx skills add leonxlnx/taste-skill@high-end-visual-design` |
| `vercel-react-best-practices` | vercel-labs/agent-skills | 3.1K | `npx skills add vercel-labs/agent-skills@vercel-react-best-practices` |
| `web-design-guidelines` | vercel-labs/agent-skills | 3.1K | `npx skills add vercel-labs/agent-skills@web-design-guidelines` |

### 🧪 测试 / 调试 / 代码质量

> 来源：`obra/superpowers` 合集

| Skill | 安装量 | 说明 | 安装命令 |
| --- | ---: | --- | --- |
| `test-driven-development` | 1.5K | TDD 流程 | `npx skills add obra/superpowers@test-driven-development` |
| `systematic-debugging` | 1.7K | 系统化调试 | `npx skills add obra/superpowers@systematic-debugging` |
| `brainstorming` | 2.2K | 头脑风暴 | `npx skills add obra/superpowers@brainstorming` |
| `writing-plans` | 1.7K | 写实现计划 | `npx skills add obra/superpowers@writing-plans` |
| `requesting-code-review` | 1.6K | 请求代码审查 | `npx skills add obra/superpowers@requesting-code-review` |
| `verification-before-completion` | 1.5K | 完成前验证 | `npx skills add obra/superpowers@verification-before-completion` |

### 📄 文档处理

> 来源：`anthropics/skills` 官方

| Skill | 安装量 | 说明 | 安装命令 |
| --- | ---: | --- | --- |
| `pptx` | 1.1K | PPT 生成 / 编辑 | `npx skills add anthropics/skills@pptx` |
| `docx` | 929 | Word 文档 | `npx skills add anthropics/skills@docx` |
| `pdf` | 874 | PDF 处理 | `npx skills add anthropics/skills@pdf` |
| `xlsx` | 803 | Excel 表格 | `npx skills add anthropics/skills@xlsx` |
| `skill-creator` | 1.5K | 创建自定义 skill | `npx skills add anthropics/skills@skill-creator` |
| `mcp-builder` | 614 | 构建 MCP 服务 | `npx skills add anthropics/skills@mcp-builder` |

### 🗄️ 数据库 / 后端

| Skill | 来源 | 安装量 | 安装命令 |
| --- | --- | ---: | --- |
| `supabase-postgres-best-practices` | supabase/agent-skills | 2.4K | `npx skills add supabase/agent-skills@supabase-postgres-best-practices` |
| `supabase` | supabase/agent-skills | 1.8K | `npx skills add supabase/agent-skills@supabase` |
| `firebase-basics` / `firebase-auth-basics` | firebase/agent-skills | ~870 | `npx skills add firebase/agent-skills@firebase-basics` |
| `better-auth-best-practices` | better-auth/skills | 664 | `npx skills add better-auth/skills@better-auth-best-practices` |
| `stripe-best-practices` | stripe/ai | 447 | `npx skills add stripe/ai@stripe-best-practices` |

### 🔧 DevOps / 部署

| Skill | 来源 | 安装量 | 安装命令 |
| --- | --- | ---: | --- |
| `microsoft-foundry` | microsoft/azure-skills | 2.3K | `npx skills add microsoft/azure-skills@microsoft-foundry` |
| `playwright-cli` | microsoft/playwright-cli | 1.0K | `npx skills add microsoft/playwright-cli@playwright-cli` |
| `deploy-to-vercel` | vercel-labs/agent-skills | 741 | `npx skills add vercel-labs/agent-skills@deploy-to-vercel` |
| `firecrawl`（系列） | firecrawl/cli | 5.1K 合计 | `npx skills add firecrawl/cli@firecrawl` |

### 🌐 飞书 / Lark 集成

> 来源：`larksuite/cli` + `open.feishu.cn`，合计超 **280K** 次安装。

涵盖 `lark-doc`、`lark-sheets`、`lark-note`、`lark-approval`、`lark-drive`、`lark-minutes` 等，覆盖飞书全家桶。

---

## 💡 针对本项目（AnuBookDEX · Go 撮合引擎）的建议

结合本项目是一个 Go 编写的加密货币现货撮合引擎，对正确性、一致性要求极高，以下 skill 相对更有价值：

| Skill | 用途 | 安装命令 |
| --- | --- | --- |
| `systematic-debugging` / `test-driven-development` | 撮合逻辑、订单簿这类对正确性要求极高的代码，用系统化调试 + TDD 保障 | `npx skills add obra/superpowers@systematic-debugging` |
| `skill-creator` | 将项目里的「快照恢复校验」「自成交预防」「精度计算规范」等约定沉淀为项目级 skill | `npx skills add anthropics/skills@skill-creator` |
| `just-scrape` | 如需做竞品 / 行情数据抓取（8.7K 安装） | `npx skills add scrapegraphai/just-scrape@just-scrape` |

