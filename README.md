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
- 适合 MVP 先用起来：不依赖 MySQL，不强制 Docker，后续可以继续扩展全文搜索、标签、导出、LDAP、Docker Compose 等能力。

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
│   ├── defaults/
│   │   └── templates/    # 系统内置 Markdown 模板，编译时打入 Go 二进制
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
    ├── templates/        # 运行时文档模板，可按公司习惯修改
    └── backups/          # 预留备份目录
```

运行数据说明：

- `data/app.db`：用户、角色、文件夹、文档标题、排序、附件、系统配置等元数据
- `data/docs/`：Markdown 文档正文，例如 `doc_1.md`
- `data/versions/`：文档历史版本快照
- `data/uploads/`：上传的图片和附件
- `data/templates/`：文档模板，首次启动时如果目录为空会从内置模板自动生成
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

服务启动时会创建 `data/templates/`。如果该目录为空，会自动写入内置 Markdown 模板；如果已经有模板文件，则不会覆盖。

如果你已有 data/app.db，说明系统已经初始化过，不会再出现向导。

想重新测试首次安装向导，不要删正式数据，可以用临时数据目录：
```bash
cd /path/to/doc.cn/server
DOC_ADDR=:8080 DOC_DATA_DIR=/tmp/doc-system-test-data go run .
```


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
```text
http://localhost:8080
```

这种方式下不需要执行 npm run dev。

#### 4.生产运行特点：

- 先构建前端，再编译 Go 后端
- 前端构建产物复制到 `server/public/`
- 线上只运行一个 Go 服务
- 浏览器访问 Go 服务地址，例如 `http://服务器IP:8080`
- 不需要运行 `npm run dev`



### 什么时候需要安装依赖

需要执行 npm install 的情况：
```bash
第一次拉项目
web/package.json 或 package-lock.json 变化后
node_modules 被删除后
```

需要执行 go mod download 的情况：
```bash
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

现在写的是服务器需要 Go、Node、npm。严格来说：构建前端时需要 Node.js + npm

编译后端时需要 Go

如果已经提前构建好了 server/public 和 server/doc-system，线上运行时只需要能运行 Go 编译出的二进制文件，不需要 Node/npm

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
```bash
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

已有系统升级部署时，只要原来的 data/app.db 还在，并且已有用户，就不会进入安装向导。服务启动时仍会检查 `data/templates/`：目录不存在或为空时自动生成内置模板；已有模板时不会覆盖。

#### 8. 一个关键注意点
当前后端静态目录写的是相对路径 public，所以线上运行时建议一定进入 server 目录再启动：
```bash
cd /path/to/doc.cn/server
DOC_ADDR=:8080 DOC_DATA_DIR=../data ./doc-system
```

否则可能找不到 server/public，导致页面打不开。

## 权限与安全概览

Doc System 默认面向内网团队使用，但权限和登录状态不是只靠前端控制，后端接口也会做校验。

| 角色 | 适合对象 | 主要权限 |
| --- | --- | --- |
| `admin` 管理员 | 系统维护者 | 管理用户、查看全部操作日志、创建和维护文档 |
| `editor` 编辑者 | 文档维护成员 | 创建、编辑、删除文档和文件夹，查看自己的操作日志 |
| `viewer` 只读用户 | 只需要查阅资料的成员 | 查看目录和文档内容，查看自己的操作日志 |

登录与安全能力：

- 登录状态使用 JWT + HttpOnly Cookie，不使用服务端内存 session。
- 未开启 MFA 时，账号密码验证成功后直接登录。
- 开启 MFA 时，账号密码验证成功后还需要输入 6 位验证码，验证通过才会登录。
- 用户自己修改密码、管理员重置密码后，旧登录状态会立即失效。
- 初始化超级管理员 `admin` 可以进入“项目配置”，普通管理员不能查看或修改项目配置。
- 管理员重置用户密码后，该用户下次登录必须先修改密码。

更细的 JWT、MFA、密码规则和权限拦截说明见：

```text
docs/project-context.md
```

## 核心能力

Doc System 当前已经可以作为团队内部知识库使用，重点能力如下：

| 能力 | 已支持 |
| --- | --- |
| 文档组织 | 飞书文档式左侧树，支持文件夹层级、拖拽排序、文档和文件夹移动，左侧栏可拖拉宽度和收起展开 |
| Markdown 写作 | 支持编辑 / 分屏 / 预览、快捷键保存、长文回到顶部、文档大纲、代码高亮、Mermaid、图片和附件上传；大纲默认关闭，可按需打开 |
| 编辑器选择 | 默认 Vditor 可视化编辑器，也可切换 ByteMD 简洁编辑器 |
| 文件导入 | 支持 Markdown、文本、HTML、Word、PDF、Excel 等文件导入并转换为 Markdown 内容 |
| 文档模板 | 内置项目架构、接口、部署、故障复盘、技术方案、工作复盘模板；首次启动会自动落地到 `data/templates/`，新建文档时可选择空白或模板 |
| 版本与恢复 | 保存文档时自动生成历史版本，支持预览恢复；删除进入回收站，可恢复或永久删除 |
| 搜索入口 | 左侧知识库上方支持按文档标题搜索，快速定位文档 |
| 用户和权限 | 支持管理员、编辑者、只读用户；权限同时在前端和后端拦截 |
| 登录安全 | JWT + HttpOnly Cookie、MFA、强制改密、密码重置后旧登录失效 |
| 项目配置 | 超级管理员可配置项目名称、JWT 有效期、新用户强制改密、MFA 失败限制 |
| 运维审计 | 操作日志记录登录、用户管理、项目配置、文档变更、回收站、历史恢复等行为 |
| 导出归档 | 支持整库 ZIP、单篇文档、单个文件夹导出；可选择 Markdown、HTML 或 PDF，并保留空文件夹结构 |
| 本地部署 | SQLite + Markdown 文件 + 本地 uploads，Go 后端可直接托管前端构建产物 |

适合第一阶段推广的使用场景：

- 项目架构、部署说明、接口文档、开发规范沉淀
- 团队工作复盘、故障处理记录、技术方案归档
- 内网环境下快速搭建轻量文档系统，不依赖云服务和 MySQL

更完整的实现细节和开发上下文见：

```text
docs/project-context.md
```

## 后续计划

短期优先：

- [x] 回收站和恢复
- [x] 更好的删除确认和错误提示
- [x] Markdown 编辑器升级为 Vditor 或 ByteMD（双编辑器可选，默认可视化编辑）
- [x] Mermaid 渲染
- [x] 代码高亮增强
- [x] 文档大纲（默认关闭，可手动打开）
- [x] PDF / Excel / 旧版 doc 导入转换

中期增强：

- [x] 文档历史版本
- [ ] 自动保存草稿
- [ ] 正文全文搜索
- [ ] 标签
- [x] 内置文档模板和初始化自动生成
- [x] 新建文档时选择空白或模板
- [x] 操作日志页面
- [ ] 定时备份
- [x] 一键导出整套知识库 ZIP
- [x] 单篇文档 / 单个文件夹导出为 Markdown、HTML 或 PDF

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
