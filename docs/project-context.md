# Doc System MVP 项目上下文

这份文档用于下次继续开发时快速理解项目背景、当前实现、运行方式和后续计划。

## 项目目标

为公司 IT 团队搭建一套简单、好用、本地部署的文档管理系统，用于记录：

- 公司项目架构
- 工作内容复盘
- 技术文档
- 部署说明
- 接口说明
- 规范沉淀

产品形态参考飞书文档、钉钉文档、MinDoc、LeafWiki、Wiki.js，但第一版只做 MVP，不追求完整企业级协作能力。

核心体验：

```text
左侧：文件夹 / 文档树
右侧：Markdown 编辑、预览、分屏
数据：本地 SQLite + 本地 Markdown 文件 + 本地附件
部署：优先直接运行 Go 二进制，Docker 后置
```

## 技术选型

当前确定方案：

```text
Go + SQLite + Markdown 文件 + Vue 3
```

后端：

- Go
- 标准库 `net/http`
- SQLite 驱动：`modernc.org/sqlite`
- 密码哈希：`golang.org/x/crypto/bcrypt`
- 登录状态：JWT + HttpOnly Cookie，服务端使用 SQLite 中的 `jwt_secret` 和用户 `token_version` 验证

前端：

- Vue 3
- Vite
- Element Plus
- ByteMD（简洁编辑，默认）
- Vditor（可视化编辑，可选）

没有使用 MySQL。Markdown 正文不存数据库。

## 当前目录结构

```text
.
├── Makefile
├── README.md
├── docs/
│   └── project-context.md
├── server/
│   ├── go.mod
│   ├── go.sum
│   ├── main.go
│   ├── doc-system        # 本地编译产物，已被 .gitignore 忽略
│   └── public/           # 前端生产构建产物，已被 .gitignore 忽略
├── web/
│   ├── index.html
│   ├── package.json
│   ├── package-lock.json
│   ├── vite.config.js
│   └── src/
│       ├── App.vue
│       ├── api.js
│       ├── main.js
│       └── styles.css
└── data/                 # 运行数据，已被 .gitignore 忽略
```

运行数据结构：

```text
data/
├── app.db
├── app.db-shm
├── app.db-wal
├── docs/
│   └── doc_1.md
├── versions/
│   └── doc_1/
│       └── v1.md
├── uploads/
└── backups/
```

## 当前实现状态

已实现：

- 登录
- 登录状态使用 JWT 维护，JWT 通过 HttpOnly Cookie 下发；未开启 MFA 时账号密码成功后下发，开启 MFA 时验证码成功后下发
- 登出
- 当前用户接口
- 登录成功 / 登录失败消息提示
- 退出登录确认和退出成功提示
- 右上角用户下拉菜单
- 角色显示为中文 + 英文，例如 `只读（viewer）`
- 退出登录入口保留在用户下拉菜单中，避免顶部重复操作
- 所有登录用户都可以在用户下拉菜单中修改自己的密码
- 项目名称可在超级管理员“项目配置”中修改，默认值为 `Doc System`，用于登录页标题和系统左上角品牌名
- 用户自己修改密码时要求至少 8 位，字母 / 数字 / 特殊符号至少包含 2 种，且不能和当前密码一致
- 用户自己修改密码成功后会强制退出，需要重新登录
- 管理员创建用户和重置密码只做基础长度校验，不强制复杂度
- 超级管理员 `admin` 可在用户下拉菜单的“项目配置”中开启“新用户首次登录强制改密”全局开关，默认关闭
- 开关开启后，新建用户首次登录只能先修改密码，后端会阻止访问其他业务接口
- 用户管理支持为单个用户开启 / 关闭 MFA
- MFA 使用 TOTP，兼容 Google Authenticator、2FAS、Aegis 等认证器 App
- 开启 MFA 且未绑定的用户登录时需要先扫码绑定，再输入 6 位验证码
- 已绑定 MFA 的用户登录时只需要输入 6 位验证码
- MFA 验证码输入框会自动聚焦，输入 6 位后自动提交验证
- MFA 验证失败限制默认是 120 秒内 5 次，达到上限后返回账号密码登录页；该限制可在“项目配置”中配置，仅超级管理员 `admin` 可查看和修改
- 管理员可重置用户 MFA，重置后该用户下次登录需要重新扫码绑定
- 管理员用户管理
- 用户列表
- 用户列表支持分页，默认每页 10 条，可切换 10 / 20 / 50 条
- 用户管理支持按用户名搜索，适合使用手机号作为用户名的场景
- 新建用户
- 编辑用户昵称和角色
- 初始化 `admin` 用户默认昵称为“超级管理员”，且不允许在用户管理中修改角色
- 重置用户密码
- 管理员重置用户密码时支持自动创建强密码，并可一键“复制密码并保存”
- 管理员重置用户密码后，该用户下次登录成功后必须先修改密码
- `admin` 账号不允许在用户管理中被重置密码，只能由当前登录的 admin 自己修改密码
- 删除用户
- 防止删除当前登录用户
- 防止移除最后一个管理员
- 角色权限：`admin`/`editor` 可编辑，`viewer` 只读
- 后端写接口按角色拦截，防止只读用户绕过前端写入
- SQLite 自动建表
- 首次启动自动创建数据目录与 SQLite 表
- 首次安装初始化向导（或通过 `DOC_ADMIN_PASSWORD` 自动创建 admin）
- 左侧树形目录
- 固定根节点：`知识库`
- 进入系统后默认展示知识库概览页
- 点击根节点展示系统目的、适用内容、项目架构和建议目录
- 根节点下新建文件夹
- 根节点下新建文档
- 文件夹下新建子文件夹
- 文件夹下新建文档
- 文件夹和根节点支持导入文件，导入后自动创建 Markdown 文档
- 导入支持 `.md`、`.markdown`、`.txt`、`.log`、`.csv`、`.html`、`.htm`、`.docx`、`.doc`、`.pdf`、`.xls`、`.xlsx`
- PDF / Excel / 旧版 `.doc`（含 RTF）支持自动转 Markdown；扫描版 PDF 或复杂二进制格式可能失败并给出明确提示
- PDF 导入默认用内置纯 Go 库（`ledongthuc/pdf` + 内嵌图提取），不依赖 Ghostscript；macOS PDFKit / Ghostscript 仅作整页预览增强
- 图片型 PDF：macOS Vision OCR / 可选 Tesseract，导入「识别文本」（可编辑）+ 页面预览图
- PDF 导入会过滤乱码文本，并保留原 PDF 下载链接
- 左侧树拖拽排序
- 文件夹和文档可以拖入文件夹，只有拖到文件夹中间区域才会进入文件夹
- 文件夹重命名
- 文件夹移入回收站，删除文件夹时会同时移动子文件夹和子文档
- 文档创建
- 文档读取
- 文档保存
- 文档历史版本：保存前若标题/正文有变化会自动快照；可预览与恢复；每篇最多保留 50 个版本
- 编辑区支持 `Ctrl + S` / `Cmd + S` 快捷键保存
- 文档重命名
- 文档移入回收站
- 删除文档和文件夹前会显示更明确的确认弹窗
- 删除文件夹时会提示影响的子文件夹和文档数量
- 回收站列表
- 回收站恢复文档和文件夹
- 回收站永久删除文档和文件夹
- 操作日志：登录用户可查看；管理员看全部，其他用户仅看自己的记录；支持筛选
- Markdown 编辑
- Markdown 预览
- 编辑 / 分屏 / 预览模式切换
- 预览模式支持窄屏、默认、宽屏三种阅读宽度
- 图片和附件上传接口
- 上传后插入 Markdown 链接
- 标题搜索，搜索框位于左侧知识库树上方，搜索结果在左侧栏展示，无结果时显示空状态提示
- 前端生产构建
- 后端静态文件托管
- Go 后端编译

根节点说明：

- 后端返回一个虚拟根节点：

```json
{
  "key": "root-0",
  "id": 0,
  "type": "root",
  "title": "知识库"
}
```

- 根节点不入库。
- 根节点不能重命名。
- 根节点不能删除。
- 根节点的子节点来自 `parent_id = 0` 的文件夹和 `folder_id = 0` 的文档。
- 根节点右侧说明页是前端内置页面，不占用 Markdown 文件。

## 当前没有实现

暂未实现：

- 复杂权限
- Git 版本管理
- 文档历史版本
- 正文全文搜索
- 评论
- 文档模板
- Mermaid 渲染
- 代码高亮增强
- 自动保存
- 冲突检测
- LDAP / 钉钉 / 企业微信登录
- Docker 部署
- 定时备份
- ~~操作日志展示~~（已实现）

## 数据存储设计

SQLite 只存元数据。

Markdown 正文存本地文件：

```text
data/docs/doc_1.md
data/docs/doc_2.md
```

附件存本地文件：

```text
data/uploads/2026/07/xxx.png
```

### SQLite 表

当前后端启动时自动创建这些表：

```text
users
folders
documents
attachments
operation_logs
```

`documents.file_path` 指向 Markdown 文件名，例如：

```text
doc_1.md
```

## API 清单

认证：

```text
GET  /api/setup/status
POST /api/setup
POST /api/login
POST /api/login/mfa
POST /api/logout
GET  /api/me
PUT  /api/me/password
GET  /api/app-config
GET  /api/settings
PUT  /api/settings
```

空库时 `GET /api/setup/status` 返回 `needed=true`，需先 `POST /api/setup` 完成初始化；可用环境变量 `DOC_ADMIN_PASSWORD` 在服务启动时自动创建 admin 并跳过向导。

`/api/settings` 仅允许初始化超级管理员 `admin` 访问；普通管理员角色不能查看或修改项目配置。

登录成功后后端写入 `doc_token` HttpOnly Cookie，内容是 HS256 签名 JWT。JWT 只包含用户 id、用户名、token_version、签发时间和过期时间；有效期由项目配置 `jwt_expire_days` 决定，默认 1 天（1～90）。每次鉴权会按用户 id 读取数据库中的当前角色和状态，并校验 token_version。用户修改密码或管理员重置密码会递增 token_version，使旧 JWT 立即失效。

用户管理：

```text
GET    /api/users
POST   /api/users
PUT    /api/users/:id
DELETE /api/users/:id
PUT    /api/users/:id/password
POST   /api/users/:id/mfa/reset
```

目录树：

```text
GET    /api/tree
PUT    /api/tree/sort
POST   /api/folders
PUT    /api/folders/:id
DELETE /api/folders/:id
```

文档：

```text
POST   /api/documents
POST   /api/documents/import
GET    /api/documents/:id
PUT    /api/documents/:id
DELETE /api/documents/:id
```

上传：

```text
POST /api/uploads
GET  /uploads/*
```

搜索：

```text
GET /api/search?q=关键词
```

操作日志（登录用户可访问；管理员看全部，其他用户仅自己的）：

```text
GET /api/operation-logs?page=1&page_size=20&action=&q=
```

## 运行方式

### 后端开发运行

```bash
cd server
go run .
```

默认配置：

```text
地址：http://localhost:8080
数据目录：../data
```

可通过环境变量修改：

```bash
DOC_ADDR=:8080 DOC_DATA_DIR=../data go run .
```

首次安装：

- 空库时打开网页进入安装向导，设置项目名称与 admin 密码后自动登录
- 也可设置环境变量 `DOC_ADMIN_PASSWORD`，服务启动时自动创建 admin 并跳过向导

```bash
DOC_ADMIN_PASSWORD=your-password go run .
```
### 前端开发运行

```bash
cd web
npm install
npm run dev
```

前端开发地址：

```text
http://localhost:5173
```

Vite 代理：

```text
/api     -> http://localhost:8080
/uploads -> http://localhost:8080
```

### 生产构建

```bash
make web-build
cd server
go build -o doc-system .
DOC_ADDR=:8080 DOC_DATA_DIR=../data ./doc-system
```

然后访问：

```text
http://localhost:8080
```

## 本地开发注意事项

当前环境里 Go 来自：

```text
/Applications/ServBay/script/alias/go
```

如果 Go 默认缓存目录无权限，可以使用项目内缓存：

```bash
GOCACHE=/Users/ck/web/datao/doc.cn/.cache/go-build \
GOMODCACHE=/Users/ck/web/datao/doc.cn/.cache/go-mod \
go build -o doc-system .
```

在中国大陆网络环境下，Go 模块下载可能需要临时使用：

```bash
GOPROXY=https://goproxy.cn,direct
```

## 首次部署步骤：

```bash
cd /path/to/doc.cn

cd web
npm install
npm run build

cd ..
rm -rf server/public
mkdir -p server/public
cp -R web/dist/. server/public/

cd server
GOPROXY=https://goproxy.cn,direct go mod download
GOPROXY=https://goproxy.cn,direct go build -o doc-system .
DOC_ADDR=:8080 DOC_DATA_DIR=../data ./doc-system
```

也可以使用 Makefile：

```bash
make web-build
make server-build
cd server
DOC_ADDR=:8080 DOC_DATA_DIR=../data ./doc-system
```

接口冒烟测试已覆盖：

- 登录
- 用户列表
- 创建用户
- 编辑用户
- 重置用户密码
- 禁止重置 admin 账号密码
- 删除用户
- 当前用户修改自己的密码
- 新用户首次登录强制改密开关
- MFA 开启 / 关闭
- MFA 重置
- viewer 保存文档返回 403
- viewer 创建文件夹返回 403
- viewer 调整目录树返回 403
- 获取目录树
- 创建文件夹
- 创建文档
- 保存文档
- 读取文档
- 标题搜索

## 后续开发优先级

建议按以下顺序继续。

### P0：补齐基础可用性

- 左侧树拖拽体验继续优化
- 上传文件大小和类型限制配置化
- 空状态优化
- Markdown 编辑体验增强

### P1：文档能力增强

- 文档历史版本（已支持：保存时快照、列表预览、一键恢复）
- Mermaid 渲染（已支持）
- 代码高亮（已支持）
- 文档大纲（已支持）
- PDF / Excel / 旧版 doc 导入转换（已支持）
- 自动保存草稿
- 正文全文搜索
- 标签
- 文档模板

### P2：团队推广能力

- 权限体系
- 多空间或多知识库
- LDAP / 钉钉 / 企业微信登录
- 操作日志页面（已支持）
- 定时备份
- 一键导出
- Docker Compose 部署
- systemd 服务文件
- 首次安装初始化向导（已支持）

### P3：长期演进

- Git 版本管理
- 静态只读站点发布
- 评论和协作
- 审批发布流程
- 文档健康度统计
- 内网 GitLab 同步

## 下次继续开发建议

下次会话优先读取：

```text
docs/project-context.md
README.md
server/main.go
web/src/App.vue
web/src/api.js
```

如果要继续做最有价值的下一步，建议从这几个需求里选一个：

```text
1. Docker Compose / systemd 部署
2. 正文全文搜索
3. 自动保存草稿
4. 上传文件大小和类型限制配置化
5. 一键导出 / 定时备份
```

当前最推荐的下一步：

```text
Docker Compose / systemd 部署，或正文全文搜索
```

原因：双编辑器、大纲、导入转换、文档历史版本、操作日志和首次安装向导已补齐，下一步可继续补部署体验或检索能力。
