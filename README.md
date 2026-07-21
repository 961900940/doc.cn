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
- 前端：Vue 3 + Vite + Element Plus + ByteMD / Vditor（双编辑器可选）
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
    ├── versions/         # 文档历史版本
    ├── uploads/          # 图片和附件
    └── backups/          # 预留备份目录
```

运行数据说明：

- `data/app.db`：用户、角色、文件夹、文档标题、排序、附件、系统配置等元数据
- `data/docs/`：Markdown 文档正文，例如 `doc_1.md`
- `data/versions/`：文档历史版本快照
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

### 方式一：本地开发环境启动
本地开发适合改页面、改接口、调试功能。前端和后端分开启动。

#### 1.安装依赖，只需要首次执行，或依赖文件变化后再执行：

```bash
cd /path/to/doc.cn/server
go mod download

cd /path/to/doc.cn/web
npm install
```

#### 2.启动后端服务：

```text
cd /path/to/doc.cn/server
DOC_ADDR=:8080 DOC_DATA_DIR=../data go run .
```

后端默认地址：

```text
http://localhost:8080
```

#### 3.启动前端开发服务：

```bash
cd /path/to/doc.cn/web
npm run dev
```

前端开发地址：

```text
http://localhost:5173
```

本地开发时浏览器访问 http://localhost:5173。Vite 会把 /api、/uploads 自动代理到 localhost:8080。

#### 4.首次安装向导怎么出现：

如果 /path/to/doc.cn/data/app.db 里没有任何用户，打开页面后会进入首次安装向导。

如果你已有 data/app.db，说明系统已经初始化过，不会再出现向导。

想重新测试首次安装向导，不要删正式数据，可以用临时数据目录：


#### 5.本地开发特点：

- 前端和后端分开运行
- 浏览器访问 `http://localhost:5173`
- 后端通过 `go run .` 临时编译并运行
- 不会生成 `server/doc-system`
- 适合日常开发、接口调试、页面调试

### 方式二：本地生产构建后启动

这个方式更接近你之前常用的 localhost:8080，不用单独启动前端 dev server。

#### 1.构建前端并复制到后端：

```bash
cd /path/to/doc.cn
make web-build
```

这个命令会执行：

```bash
cd web && npm run build
rm -rf server/public
mkdir -p server/public
cp -R web/dist/. server/public/
```

也就是把 Vue 前端打包后放进 server/public，由 Go 后端直接托管。


#### 2.编译后端：

```bash
cd /path/to/doc.cn
make server-build
```

会生成：
```bash
server/doc-system
```

#### 3.启动服务：

```bash
cd /path/to/doc.cn/server
DOC_ADDR=:8080 DOC_DATA_DIR=../data ./doc-system
```

浏览器访问：

这种方式下不需要执行 npm run dev。

#### 4.生产运行特点：

- 先构建前端，再编译 Go 后端
- 前端构建产物复制到 `server/public/`
- 线上只运行一个 Go 服务
- 浏览器访问 Go 服务地址，例如 `http://服务器IP:8080`
- 不需要运行 `npm run dev`



### 什么时候需要安装依赖

需要执行 npm install 的情况：
```php
第一次拉项目
web/package.json 或 package-lock.json 变化后
node_modules 被删除后
```

需要执行 go mod download 的情况：
```php
第一次拉项目
server/go.mod 或 server/go.sum 变化后
Go 依赖缓存被清理后
```
平时改业务代码，不一定每次都要重新安装依赖。

### 什么时候不需要 Go 编译：

- 本地开发
- 快速验证后端代码
- 调试接口
- 配合前端 Vite 开发服务调试页面

这种情况下使用：

```bash
cd server
go run .
```
不需要手动 go build，Go 会临时编译并运行。

### 什么时候需要 Go 编译：

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

需要编译，生成稳定的可执行文件 doc-system，再用 ./doc-system 运行。

前端代码改了，线上必须重新执行：
```bash
make web-build
```

后端代码改了，线上必须重新执行：
```bash
make server-build
```
前后端都改了，就两个都执行，然后重启服务。

### 线上 安装 / 部署 流程

线上推荐用“生产式启动”：只运行一个 Go 服务。

#### 1.环境要求-服务器需要安装：

- Go 1.22+
- Node.js 18+
- npm

如果执行 `go mod download` 时出现 `proxy.golang.org` 超时，可以先设置 Go 依赖下载代理：

```bash
go env -w GOPROXY=https://goproxy.cn,direct
```

#### 2.首次拉代码：

```bash
git clone <你的仓库地址> doc.cn
cd doc.cn
```

#### 3.安装依赖：

```bash
cd web
npm install
```

```bash
cd ../server
go mod download
```

#### 4.构建前端：

```bash
cd /path/to/doc.cn
make web-build
```

#### 5.编译后端：

```bash
make server-build
```

#### 6.启动线上服务：

```bash
cd /path/to/doc.cn/server
DOC_ADDR=:8080 DOC_DATA_DIR=../data ./doc-system
```

访问：
```text
http://服务器IP:8080
```

如果你绑定了域名，可以用 Nginx 反向代理到 127.0.0.1:8080。

#### 7. 线上首次安装向导

线上第一次部署时，如果 data/app.db 不存在或 users 表为空：

* 不设置 DOC_ADMIN_PASSWORD：第一次访问系统会进入安装向导。
* 设置 DOC_ADMIN_PASSWORD：服务启动时自动创建 admin，跳过安装向导。

例如：
```bash
cd /path/to/doc.cn/server
DOC_ADDR=:8080 DOC_DATA_DIR=../data DOC_ADMIN_PASSWORD='Admin123!' ./doc-system
```

自动创建：

```bash
用户名：admin
昵称：超级管理员
密码：DOC_ADMIN_PASSWORD 的值
```

已有系统升级部署时，只要原来的 data/app.db 还在，并且已有用户，就不会进入安装向导。

#### 8. 一个关键注意点
当前后端静态目录写的是相对路径 public，所以线上运行时建议一定进入 server 目录再启动：
```bash
cd /path/to/doc.cn/server
DOC_ADDR=:8080 DOC_DATA_DIR=../data ./doc-system
```

否则可能找不到 server/public，导致页面打不开。



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


## 推送到 Git 前处理

当前项目可以作为初始化项目推送到 Git，建议只提交源码、配置说明和文档，不提交本地运行数据。

刚从 Git 拉取项目后，如果没有看到以下目录或文件，这是正常的：

- `server/public/`：前端生产构建后才会生成
- `server/doc-system`：后端编译后才会生成
- `web/dist/`：前端执行 `npm run build` 后才会生成
- `web/node_modules/`：前端执行 `npm install` 后才会生成
- `data/`：服务首次启动后会按需创建，属于本地运行数据

如果新拉取的项目里已经有 `data/app.db`、`data/docs/`、`data/uploads/`，说明本地运行数据可能被提交到了 Git。推广给别人使用时不建议提交这些文件，因为里面可能包含用户、密码哈希、JWT 密钥、MFA 配置、公司文档和上传附件。


不应该提交：

- `data/`：包含 SQLite 数据库、文档正文、上传附件、JWT 密钥和本地用户数据
- `web/node_modules/`：前端依赖目录，可通过 `npm install` 重新安装
- `web/dist/`：前端构建产物
- `server/public/`：复制给 Go 服务托管的前端构建产物
- `server/doc-system`：后端编译产物
- `.cache/`：本地 Go 缓存
- `.idea/`：本地 IDE 配置
- `*.log`、`*.tmp`、`*-副本.*` 等本地临时文件



## 角色权限

系统当前有三种角色：

```text
admin   管理员：可管理用户、查看全部操作日志，可创建、编辑、删除文档和文件夹
editor  编辑者：可创建、编辑、删除文档和文件夹，可查看自己的操作日志，不能管理用户
viewer  只读用户：只能查看目录和文档内容，可查看自己的操作日志
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
- JWT 有效期默认 1 天，可在项目配置中调整（1～90 天），修改后仅对新登录生效
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

- 首次安装初始化向导（空库时引导设置项目名与 admin 密码；可用 `DOC_ADMIN_PASSWORD` 跳过）
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
- JWT 登录有效期配置，默认 1 天（范围 1～90 天），修改后仅对新登录生效
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
- 为用户开启 / 关闭 MFA（关闭时会同时清除 MFA 密钥与绑定时间）
- 重置用户 MFA
- 防止删除当前登录用户
- 防止移除最后一个管理员
- 防止在用户管理中重置 `admin` 密码
- 防止在用户管理中修改初始化 `admin` 角色

操作日志：

- 所有登录用户可查看操作日志
- 管理员可查看全部用户的记录；其他用户只能查看自己的记录
- 记录登录 / 登出、用户管理、项目配置、文档与文件夹变更、回收站、历史版本恢复、附件上传等
- 支持按操作类型和关键词筛选，分页浏览

文档目录：

- 左侧文件夹 / 文档树
- 固定根节点 `知识库`
- 根节点下新建文件夹和文档
- 文件夹下新建子文件夹和文档
- 文件夹和根节点支持导入文件并转成 Markdown 文档
- 导入支持 `.md`、`.markdown`、`.txt`、`.log`、`.csv`、`.html`、`.htm`、`.docx`、`.doc`、`.pdf`、`.xls`、`.xlsx`
- PDF 提取正文文本（扫描件/加密件可能失败）
- PDF 优先按页渲染为图片导入（保留文字/表格/图片版面），并附原 PDF 下载链接
- PDF 默认用内置纯 Go 库提取文本/内嵌图片，**不依赖** Ghostscript；Windows 开箱可用
- 图片型/扫描件 PDF：macOS 用系统 Vision OCR 识别可编辑文字；Windows/Linux 可安装 Tesseract（`chi_sim`）增强
- 若环境有 PDFKit（macOS）或 Ghostscript，会额外生成整页预览（对照用）；正文优先「识别文本」
- Excel 转 Markdown 表格（多工作表会按标题分段）
- 旧版 `.doc` / RTF 尽量提取文本；复杂排版建议另存 `.docx`
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
- 文档历史版本：保存有变化时自动快照，可预览并恢复（每篇最多 50 个）
- 编辑区支持 `Ctrl + S` / `Cmd + S` 快捷键保存
- 双编辑器可选：默认「简洁编辑」（ByteMD），也可切换「可视化编辑」（Vditor）
- 左上角项目名称旁可切换编辑器，选择会保存在浏览器本地
- 编辑 / 分屏 / 预览模式切换
- Markdown 预览
- 预览模式支持窄屏、默认、宽屏三种阅读宽度
- 文档大纲：根据 Markdown 标题自动生成，点击可跳转
- 文档标题栏可开关大纲面板，偏好保存在本地
- 图片和附件上传
- 上传后自动插入 Markdown 链接
- 内置 GFM、代码高亮与 Mermaid 支持（随所选编辑器）

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
- 首次打开可走安装向导创建超级管理员（或通过 `DOC_ADMIN_PASSWORD` 自动创建）
- 本地运行数据目录自动创建
- 后端托管前端生产构建产物
- 支持通过环境变量配置监听地址和数据目录

## 后续计划

短期优先：

- [x] 回收站和恢复
- [x] 更好的删除确认和错误提示
- [x] Markdown 编辑器升级为 Vditor 或 ByteMD（双编辑器可选，默认简洁编辑）
- [x] Mermaid 渲染
- [x] 代码高亮增强
- [x] 文档大纲
- [x] PDF / Excel / 旧版 doc 导入转换

中期增强：

- [x] 文档历史版本
- [ ] 自动保存草稿
- [ ] 正文全文搜索
- [ ] 标签
- [ ] 文档模板
- [x] 操作日志页面
- [ ] 定时备份
- [ ] 一键导出

团队推广：

- [ ] 复杂权限体系
- [ ] 多知识库或多空间
- [ ] LDAP / 钉钉 / 企业微信登录
- [ ] Docker Compose 部署
- [ ] systemd 服务文件
- [x] 首次安装初始化向导

长期演进：

- [ ] Git 版本管理
- [ ] 评论和协作
- [ ] 审批发布流程
- [ ] 静态只读站点发布
- [ ] 文档健康度统计
- [ ] 内网 GitLab 同步
