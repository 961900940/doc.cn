# Doc System

## 项目介绍

Doc System 是一套本地部署的团队 Markdown 文档管理系统，目标是给公司 IT 团队快速搭建一个轻量、简单、可长期演进的内部知识库。

系统形态类似“飞书文档式左侧树 + Markdown 内容区”：左侧是文件夹 / 文档树，右侧是 Markdown 文档阅读、编辑和预览区域。适合记录项目架构、技术方案、部署说明、接口文档、工作复盘、开发规范等内容。

继续开发前建议先阅读：

```text
docs/project-context.md
```

## 核心特点

- 本地部署：所有数据保存在本机或内网服务器，不依赖云端服务。
- 部署简单：生产环境可由 Go 后端直接托管前端构建产物，只需要启动一个 Go 服务。
- 数据清晰：SQLite 存元数据，Markdown 文件存正文，上传文件存本地目录。
- 使用直观：左侧文件夹层级树，右侧 Markdown 内容区，进入系统默认展示知识库说明页。
- 权限可控：支持管理员、编辑者、只读用户，后端接口也会做权限拦截。
- 安全登录：登录状态使用 JWT + HttpOnly Cookie，支持 MFA、强制改密、密码重置后旧登录失效。
- 适合 MVP 先用起来：不依赖 MySQL，不强制 Docker，后续可以继续扩展版本管理、全文搜索、导出、LDAP 等能力。

## 技术架构

```text
浏览器 / Vue 3 / Element Plus
        |
        | HTTP API
        v
Go 后端服务
        |
        | 读写
        v
SQLite + Markdown 文件 + 本地 uploads
```

技术栈：

- 后端：Go 标准库 HTTP 服务
- 数据库：SQLite，使用 `modernc.org/sqlite`
- 正文存储：本地 Markdown 文件
- 附件存储：本地 `uploads`
- 前端：Vue 3 + Vite + Element Plus + markdown-it
- MFA：TOTP，兼容 Google Authenticator、2FAS、Aegis 等认证器 App

## 目录结构

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
│   ├── doc-system        # 后端编译产物
│   └── public/           # 前端生产构建产物，由 make web-build 生成
├── web/
│   ├── package.json
│   ├── src/
│   │   ├── App.vue
│   │   ├── api.js
│   │   └── styles.css
│   └── dist/             # Vite 构建产物
└── data/
    ├── app.db            # SQLite 数据库
    ├── docs/             # Markdown 正文
    ├── uploads/          # 图片和附件
    └── backups/          # 预留备份目录
```

运行数据说明：

- `data/app.db`：用户、角色、文件夹、文档标题、排序、附件、系统配置等元数据
- `data/docs/`：Markdown 文档正文，例如 `doc_1.md`
- `data/uploads/`：上传的图片和附件
- `data/backups/`：预留备份目录

## 如何启动

### 新拉取项目后的文件状态

刚从 Git 拉取项目后，通常只会看到源码、README、Makefile 和依赖清单，不会看到运行时目录和构建产物。

这些文件或目录缺失是正常的：

- `data/`：服务首次启动后按需创建，保存 SQLite、Markdown 正文和上传附件
- `web/node_modules/`：执行 `npm install` 后生成
- `web/dist/`：执行 `npm run build` 后生成
- `server/public/`：生产构建时由 `web/dist` 复制生成
- `server/doc-system`：执行 `go build` 后生成的后端可执行文件

### 方式一：本地开发启动

启动后端：

```bash
cd server
GOPROXY=https://goproxy.cn,direct go mod download
go run .
```

后端默认地址：

```text
http://localhost:8080
```

启动前端开发服务：

```bash
cd web
npm install
npm run dev
```

前端开发地址：

```text
http://localhost:5173
```

Vite 会把 `/api` 和 `/uploads` 代理到后端 `localhost:8080`。

本地开发特点：

- 前端和后端分开运行
- 浏览器访问 `http://localhost:5173`
- 后端通过 `go run .` 临时编译并运行
- 不会生成 `server/doc-system`
- 适合日常开发、接口调试、页面调试

### 方式二：生产构建后启动

在项目根目录执行：

```bash
make web-build
make server-build
cd server
DOC_ADDR=:8080 DOC_DATA_DIR=../data ./doc-system
```

然后访问：

```text
http://localhost:8080
```

生产运行特点：

- 先构建前端，再编译 Go 后端
- 前端构建产物复制到 `server/public/`
- 线上只运行一个 Go 服务
- 浏览器访问 Go 服务地址，例如 `http://服务器IP:8080`
- 不需要运行 `npm run dev`

什么时候不需要 Go 编译：

- 本地开发
- 快速验证后端代码
- 调试接口
- 配合前端 Vite 开发服务调试页面

这种情况下使用：

```bash
cd server
go run .
```

什么时候需要 Go 编译：

- 线上部署
- 长期后台运行
- 给别人提供可执行服务
- 配合 `systemd`、`supervisor`、宝塔、`nohup` 等方式运行

这种情况下使用：

```bash
cd server
go build -o doc-system .
DOC_ADDR=:8080 DOC_DATA_DIR=../data ./doc-system
```

## 安装 / 部署

环境要求：

- Go 1.22+
- Node.js 18+
- npm

如果执行 `go mod download` 时出现 `proxy.golang.org` 超时，可以先设置 Go 依赖下载代理：

```bash
go env -w GOPROXY=https://goproxy.cn,direct
```

首次部署步骤：

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

常用环境变量：

```text
DOC_ADDR=:8080          # 服务监听地址
DOC_DATA_DIR=../data    # 数据目录
DOC_ADMIN_PASSWORD=...  # 首次初始化 admin 时使用的默认密码
```

首次启动会自动：

- 创建 SQLite 表
- 创建数据目录
- 创建默认管理员账号
- 生成 JWT 签名密钥 `jwt_secret`

默认管理员：

```text
用户名：admin
昵称：超级管理员
密码：admin123
```

说明：

- 当前版本不强制 Docker 部署。
- SQLite 文件、Markdown 文件和 uploads 目录都在本地，部署时需要保留 `data/` 目录。
- 如果要迁移服务器，复制项目代码和 `data/` 目录即可。

## 后续修改代码如何操作

后端代码修改后：

```bash
cd server
go fmt ./...
go build -o doc-system .
```

前端代码修改后：

```bash
cd web
npm run build
```

把前端构建产物复制给后端托管：

```bash
cd ..
rm -rf server/public
mkdir -p server/public
cp -R web/dist/. server/public/
```

推荐直接使用：

```bash
make web-build
make server-build
```

重启服务：

```bash
cd server
DOC_ADDR=:8080 DOC_DATA_DIR=../data ./doc-system
```

如果 Go 默认缓存目录无权限，可以使用项目内缓存：

```bash
cd server
GOCACHE=/Users/ck/web/datao/doc.cn/.cache/go-build \
GOMODCACHE=/Users/ck/web/datao/doc.cn/.cache/go-mod \
go build -o doc-system .
```

## 推送到 Git 前处理

当前项目可以作为初始化项目推送到 Git，建议只提交源码、配置说明和文档，不提交本地运行数据。

刚从 Git 拉取项目后，如果没有看到以下目录或文件，这是正常的：

- `server/public/`：前端生产构建后才会生成
- `server/doc-system`：后端编译后才会生成
- `web/dist/`：前端执行 `npm run build` 后才会生成
- `web/node_modules/`：前端执行 `npm install` 后才会生成
- `data/`：服务首次启动后会按需创建，属于本地运行数据

如果新拉取的项目里已经有 `data/app.db`、`data/docs/`、`data/uploads/`，说明本地运行数据可能被提交到了 Git。推广给别人使用时不建议提交这些文件，因为里面可能包含用户、密码哈希、JWT 密钥、MFA 配置、公司文档和上传附件。

应该提交：

- `README.md`
- `docs/project-context.md`
- `Makefile`
- `server/go.mod`
- `server/go.sum`
- `server/main.go`
- `web/package.json`
- `web/package-lock.json`
- `web/index.html`
- `web/vite.config.js`
- `web/src/`
- `.gitignore`

不应该提交：

- `data/`：包含 SQLite 数据库、文档正文、上传附件、JWT 密钥和本地用户数据
- `web/node_modules/`：前端依赖目录，可通过 `npm install` 重新安装
- `web/dist/`：前端构建产物
- `server/public/`：复制给 Go 服务托管的前端构建产物
- `server/doc-system`：后端编译产物
- `.cache/`：本地 Go 缓存
- `.idea/`：本地 IDE 配置
- `*.log`、`*.tmp`、`*-副本.*` 等本地临时文件

初始化 Git 仓库：

```bash
git init
git add .
git status
git commit -m "init doc system"
```

推送到远程仓库：

```bash
git remote add origin <your-git-repo-url>
git branch -M main
git push -u origin main
```

推送前建议确认：

- `git status` 中没有 `data/`
- `git status` 中没有 `web/node_modules/`
- `git status` 中没有 `web/dist/`
- `git status` 中没有 `server/public/`
- `git status` 中没有 `server/doc-system`
- 默认管理员密码只作为本地初始化说明，不把真实生产密码写入仓库

## 基础使用

1. 使用默认管理员登录。

```text
用户名：admin
密码：admin123
```

2. 进入系统后默认展示知识库说明页。

说明页会展示系统目的、适合记录的内容、当前项目架构和建议目录。

3. 在左侧知识库树中管理目录。

支持新建文件夹、新建文档、文件夹重命名、文件夹软删除、文档软删除、拖拽排序。

4. 在右侧编辑 Markdown 文档。

支持编辑、分屏、预览三种模式。编辑区支持 `Ctrl + S` / `Cmd + S` 快捷键保存。

5. 上传图片或附件。

上传后会自动插入 Markdown 链接。

6. 使用标题搜索。

搜索框在左侧知识库树上方，只搜索文档标题。搜索结果在左侧展示，无结果时显示空状态提示。

7. 管理用户。

管理员可以在右上角用户菜单进入“用户管理”，支持新建用户、编辑用户、重置密码、开启 / 关闭 MFA、重置 MFA、删除用户、分页和按用户名搜索。一般可以直接使用手机号作为用户名。

8. 配置项目。

只有初始化超级管理员 `admin` 可以看到“项目配置”。可配置项目名称、新用户首次登录强制改密、MFA 失败限制。

## 角色权限

系统当前有三种角色：

```text
admin   管理员：可管理用户，可创建、编辑、删除文档和文件夹
editor  编辑者：可创建、编辑、删除文档和文件夹，不能管理用户
viewer  只读用户：只能查看目录和文档内容
```

权限不仅在前端隐藏按钮，也在后端接口中拦截。

`viewer` 访问以下写操作会返回 `403`：

- 保存文档
- 创建 / 删除文档
- 创建 / 修改 / 删除文件夹
- 调整目录树
- 上传附件

管理员限制：

- 初始化 `admin` 用户默认昵称为“超级管理员”
- `admin` 用户不允许在用户管理中修改角色
- `admin` 用户不允许在用户管理中被重置密码，只能当前登录的 admin 自己修改密码
- 不能删除当前登录用户
- 不能移除最后一个管理员
- 普通管理员角色可以管理用户，但不能查看或修改“项目配置”
- 只有初始化超级管理员 `admin` 可以访问 `/api/settings`

## 认证与登录状态

当前登录状态使用 JWT，不再使用服务端内存 session。

- 登录成功后后端写入 `doc_token` HttpOnly Cookie
- JWT 使用 HS256 签名
- JWT 签名密钥 `jwt_secret` 存在本地 SQLite 的 `app_settings` 表中，首次启动自动生成
- JWT 有效期为 7 天
- JWT 内容包含用户 id、用户名、`token_version`、签发时间和过期时间
- 每次鉴权都会按 JWT 中的用户 id 重新读取数据库里的当前用户角色、MFA 状态和强制改密状态
- 未开启 MFA：账号密码验证成功后立即下发 `doc_token`
- 开启 MFA：账号密码验证成功后只返回 MFA challenge，不下发 token；MFA 验证码验证成功后才下发 `doc_token`
- 用户自己修改密码、管理员重置用户密码后，会递增该用户的 `token_version`，旧 JWT 立即失效
- 退出登录会清理 `doc_token` Cookie；旧版 `doc_session` Cookie 也会被兼容清理

JWT 保存位置：

- JWT 生成后不保存到服务端数据库
- 后端通过 `Set-Cookie` 写入浏览器 Cookie
- Cookie 名称是 `doc_token`
- `doc_token` 设置为 `HttpOnly`，前端 JavaScript 不能直接读取
- 服务端真正保存的是 `app_settings.jwt_secret` 和 `users.token_version`

后续请求验证流程：

```text
浏览器请求接口
    |
    | Cookie: doc_token=xxx
    v
后端读取 doc_token
    |
    v
校验 JWT 签名
    |
    v
检查 JWT 是否过期
    |
    v
根据 JWT 里的用户 id 查询数据库用户
    |
    v
比较 JWT 中的 token_version 和数据库中的 token_version
    |
    v
验证通过后进入业务接口
```

旧登录失效机制：

- 用户自己修改密码后，后端会递增该用户的 `token_version`
- 管理员重置用户密码后，后端也会递增该用户的 `token_version`
- 旧 JWT 里的 `token_version` 和数据库不一致时，请求会返回登录失效
- 这种方式不需要服务端保存每一个 token，也能让旧登录状态立即失效

MFA 说明：

- MFA 使用 TOTP
- 未绑定 MFA 的用户首次通过账号密码后，会显示二维码和手动密钥
- 用户扫描绑定后输入 6 位验证码，校验成功才登录
- 已绑定 MFA 的用户后续登录只需要输入验证码
- MFA 验证码输入框自动聚焦，输入 6 位后自动提交
- MFA 失败限制默认是 120 秒内 5 次，可在项目配置中调整
- 达到失败上限后，会返回账号密码登录页面

MFA 技术实现：

- MFA 验证码本身没有使用专门的第三方 TOTP 库，是后端在 `server/main.go` 里按 TOTP 标准实现的
- 第三方库 `github.com/skip2/go-qrcode` 只用于生成 MFA 绑定时扫码用的二维码
- TOTP 验证码生成和校验使用 Go 标准库：`crypto/hmac`、`crypto/sha1`、`encoding/base32`、`encoding/binary`
- 兼容 Google Authenticator、Microsoft Authenticator、Aegis、2FAS 等标准 TOTP 认证器 App
- 不依赖外部 MFA 服务，也不会把 MFA 数据上传到第三方

密码规则：

- 管理员新建用户和重置密码只要求基础长度，便于快速创建账号
- 用户自己修改密码时必须至少 8 位
- 用户自己修改密码时，字母 / 数字 / 特殊符号至少包含 2 种
- 用户自己修改密码时，新密码不能和当前密码一致
- 管理员重置密码后，该用户下次登录成功后必须先修改密码

## 已实现功能列表

认证和账户：

- 登录、登出、当前用户信息
- JWT + HttpOnly Cookie 登录状态
- 未开启 MFA 时账号密码成功后下发 JWT
- 开启 MFA 时验证码成功后下发 JWT
- 登录成功 / 失败消息提示
- 退出登录确认和退出成功提示
- 所有用户可修改自己的密码
- 修改自己密码后强制退出重新登录
- 新用户首次登录强制改密开关
- 管理员重置密码后强制用户下次登录改密

项目配置：

- 项目名称配置，默认 `Doc System`
- 新用户首次登录强制改密开关，默认关闭
- MFA 失败限制配置，默认 120 秒内 5 次
- 项目配置仅初始化超级管理员 `admin` 可见可改
- 公开只读项目配置接口 `/api/app-config`

用户管理：

- 用户列表
- 用户列表分页，默认每页 10 条
- 用户列表分页大小支持 10 / 20 / 50
- 按用户名模糊搜索
- 新建用户
- 编辑用户昵称和角色
- 删除用户
- 重置用户密码
- 自动创建强密码
- 复制密码并保存
- 为用户开启 / 关闭 MFA
- 重置用户 MFA
- 防止删除当前登录用户
- 防止移除最后一个管理员
- 防止在用户管理中重置 `admin` 密码
- 防止在用户管理中修改初始化 `admin` 角色

文档目录：

- 左侧文件夹 / 文档树
- 固定根节点 `知识库`
- 根节点下新建文件夹和文档
- 文件夹下新建子文件夹和文档
- 文件夹重命名
- 删除文档 / 文件夹会先移入回收站
- 删除文件夹时会提示影响范围，并连同子文件夹、子文档一起移入回收站
- 回收站支持列表查看、恢复、永久删除
- 删除 / 恢复失败时展示后端返回的具体错误原因
- 左侧树拖拽排序
- 文件夹和文档支持拖入文件夹
- 拖拽时区分排序和进入文件夹：上 / 下位置用于排序，中间区域用于进入文件夹
- 禁止拖动根节点
- 禁止拖入文档节点
- 防止文件夹移动到自己的子级中

文档编辑：

- Markdown 文档创建、读取、保存、重命名
- 编辑区支持 `Ctrl + S` / `Cmd + S` 快捷键保存
- 编辑 / 分屏 / 预览模式切换
- Markdown 预览
- 预览模式支持窄屏、默认、宽屏三种阅读宽度
- 图片和附件上传
- 上传后自动插入 Markdown 链接

知识库首页和搜索：

- 进入系统后默认展示知识库概览页
- 点击根节点展示系统目的、适用内容、项目架构和建议目录
- 概览页是前端内置页面，不占用 Markdown 文件
- 文档标题搜索
- 搜索框位于左侧知识库树上方
- 搜索结果在左侧栏展示
- 无搜索结果时显示空状态提示

本地运行：

- 首次启动自动初始化 SQLite 表
- 首次启动自动创建默认管理员账号
- 本地运行数据目录自动创建
- 后端托管前端生产构建产物
- 支持通过环境变量配置监听地址和数据目录

## 后续计划

短期优先：

- [x] 回收站和恢复
- [x] 更好的删除确认和错误提示
- [ ] Markdown 编辑器升级为 Vditor 或 ByteMD
- [ ] Mermaid 渲染
- [ ] 代码高亮增强
- [ ] 文档大纲

中期增强：

- [ ] 文档历史版本
- [ ] 自动保存草稿
- [ ] 正文全文搜索
- [ ] 标签
- [ ] 文档模板
- [ ] 操作日志页面
- [ ] 定时备份
- [ ] 一键导出

团队推广：

- [ ] 复杂权限体系
- [ ] 多知识库或多空间
- [ ] LDAP / 钉钉 / 企业微信登录
- [ ] Docker Compose 部署
- [ ] systemd 服务文件
- [ ] 首次安装初始化向导

长期演进：

- [ ] Git 版本管理
- [ ] 评论和协作
- [ ] 审批发布流程
- [ ] 静态只读站点发布
- [ ] 文档健康度统计
- [ ] 内网 GitLab 同步
