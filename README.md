# ZXY Panel

ZXY Panel 是一个面向跨境业务网络节点管理的控制台，用于统一管理专线模式、中转入口、落地出口、固定出口客户、SOCKS5 路由中转、Clash/Mihomo 订阅和网络核心状态。

## 当前版本

- 源码开发版本：`0.7.8-stable-engineering`
- 正式发布版本：`0.7.8-stable-engineering`；标签 `v0.7.8`，包名 `zxy-panel-v0.7.8-stable-engineering.zip`。
- 发布时间：`2026-10-07`；实际下载和 Latest 状态以 GitHub Release 为准，保留 `v0.7.8-test.1` 测试预发布及历史证据。
- V0.7.8 已验证范围：Ubuntu 22.04、Linux amd64、fast/systemd，升级起点为 V0.7.7.5。
- Debian 12、Docker、更早版本升级和整机快照回滚未完成本轮真机验收，不扩大兼容承诺。
- 正式发布计划、验收证据边界和已知问题见 [V0.7.8 发布说明](docs/releases/v0.7.8.md)。

## 功能列表

- 跨境业务网络节点管理
- 专线模式客户管理
- 中转入口与落地出口配置
- 固定出口客户保持一客户一入口一出口
- 直连客户与固定出口客户分流管理
- SOCKS5 路由中转
- 客户专属入口与固定出口能力
- VLESS Reality 入站与客户绑定
- Clash Verge / Mihomo 远程订阅：`/sub/<token>?format=clash`
- Clash YAML 下载：`/sub/<token>?format=clash&download=1`
- 端口、UUID、SOCKS5 账号绑定一致性检测
- Agent 下发、写入、重启后的配置一致性校验
- 网络核心版本展示，优先读取 Agent 上报版本
- 系统升级页与远程版本清单配置
- 生成升级命令与更新检查入口

## 一键安装

以下公开入口读取远端 main 的正式清单，V0.7.8 清单指向 `v0.7.8` Release 的原样验收包。V0.7.8 已验证 Ubuntu 22.04、Linux amd64、fast/systemd 的安装、重复安装、配置恢复和升级；Debian/Docker 及整机快照回滚尚未验收。旧系统、ARM、跨模式迁移及 Docker 外部数据库不在本轮已验证范围内。

```bash
bash <(curl -Ls https://raw.githubusercontent.com/zoow1388-svg/zxy-panel/main/install.sh)
```

也可以指定版本清单地址：

```bash
ZXY_UPDATE_MANIFEST_URL=https://raw.githubusercontent.com/zoow1388-svg/zxy-panel/main/version.json \
bash <(curl -Ls https://raw.githubusercontent.com/zoow1388-svg/zxy-panel/main/install.sh)
```

## 一键升级

该命令检查清单并打印待审核的升级命令，不会立即安装。生成命令先验证 SHA256 和 ZIP 路径，再执行安装器；安装失败不会被日志管道隐藏。相同或更高版本不生成降级命令，自定义 APP_DIR/CONFIG_DIR 会带入命令。详见 [升级说明](docs/UPGRADE.md)。

```bash
zxy-panel update
```

也可以重新执行公开安装入口，读取正式清单；不得将它当作本地候选的测试入口：

```bash
bash <(curl -Ls https://raw.githubusercontent.com/zoow1388-svg/zxy-panel/main/install.sh)
```

## 常用命令

```bash
zxy-panel info
zxy-panel status
zxy-panel restart
zxy-panel logs
zxy-panel doctor
zxy-panel backup
zxy-panel backup-list
zxy-panel update
zxy-panel uninstall
```

## V0.7.8 发布说明

- 保留 BBR、版本治理及 R1/R1.5 Server、Node、Relay、Client 绑定护栏和保存失败保护。
- 完善 Node/Relay 错误提示及保存状态，避免重复提交和草稿丢失。
- 修正 systemd 服务路径和严格 Xray drop-in 归属检查，支持已验收的重复安装和配置/数据恢复。
- Ubuntu 22.04 amd64 fast/systemd 的 V0.7.7.5 手动升级、后台托管升级、旧数据保留、原客户端联网、重启恢复及 300 秒空闲观察通过。
- 原样使用已验收 ZIP，不重建二进制、前端或安装包。当前源码的发布文档更新不在该 ZIP 内，完整差异在包外说明中列明。
- BBR 安装日志可能误提示未开启；网络波动原因未确认。完整限制与证据边界见 [发布说明](docs/releases/v0.7.8.md)。

## V0.7.7.5 历史发布说明

V0.7.7.5 是基于 V0.7.7.4 的稳定性收尾版本，不增加高风险新功能，重点处理测试过程中反复出现的发布一致性、客户端导入误判和安装自检提示问题：

- 统一 README、CHANGELOG、构建脚本、前端文案、后端版本、Agent 版本和安装脚本版本。
- `scripts/build-fast-release.sh` 支持动态 VERSION / CODENAME，默认打包 V0.7.7.5 stability-polish。
- Release 包不再内置带 SHA256 的 `version.json`，避免发布包内部 manifest 与外部 release manifest 互相引用导致 hash 不一致。
- Fresh install 没有备份、尚未创建客户绑定时，`zxy-panel doctor` 改为信息提示，不再误报 warning。
- 客户分享弹窗补充 Clash Verge / Mihomo 使用提示，明确订阅源端口不是节点端口、导入后需在代理页选节点、内核通信错误时应重启内核或客户端进程。
- Clash YAML 顶部增加客户端使用说明，减少“导入成功但当前节点为空”的误判。
- 保留 V0.7.7.3 Clash/Mihomo 订阅绑定修复，多客户多端口订阅不再混淆。
- 保留 V0.7.7.2 端口 UUID BindingCheck、Agent apply 校验、Doctor 深度检测和客户 merge update。

## 安装后自检

安装或升级完成后执行：

```bash
zxy-panel doctor
```

doctor 检查实际部署模式、端口、可选组件、数据库及 API/Nginx 健康 JSON 和版本。无备份或未安装可选组件显示 INFO；PASS 不等于客户端联网、绑定深度核验或真实部署验收通过。基本健康检查通过时包含：

```text
Doctor result: PASS
```

## 配置与数据恢复

配置/数据备份不包含程序。恢复只接受本 CLI 新格式、同安装目录/配置目录/数据库映射及同模式的可信备份，保留当前程序、服务定义和 Nginx 路由。旧式系统根目录归档不自动解压。整版回滚须使用预先验证的完整虚拟机和数据盘快照，不要用旧包重新安装代替回滚。

## Clash Verge / Mihomo 使用提醒

Clash Verge 首页显示的“来源：服务器IP:面板端口”是订阅源地址，不是节点转发端口。真正的节点端口需要到“代理”页面或订阅 YAML 的 `port` 字段查看。

导入 Clash 订阅后，如首页显示“暂无激活的代理节点”或“内核通信错误”，请先：

1. 进入“代理”页面手动选择 `PROXY` 分组里的节点。
2. 重启 Mihomo / Clash 内核。
3. Windows 环境可尝试以管理员身份启动 Clash Verge。
4. 如仍无流量，先关闭 TUN，只开启系统代理测试；确认可用后再开启 TUN。

## 发布一致性说明

`VERSION` 是源码版本基准。`version.json` 是一键安装和一键升级读取的发布清单。V0.7.8 的清单、运行版本与已验收包 SHA256 一致；只有正式附件下载核验通过后才投放 main 清单。

以下为正式清单元数据；最低声明升级起点为本轮实际验收的 V0.7.7.5。该字段不是客户端强制升级门槛，不代表更早版本兼容性已通过：

- 最新已发布版本：`0.7.8-stable-engineering`
- `latest`: `0.7.8-stable-engineering`
- `version`: `0.7.8`
- `codename`: `stable-engineering`
- `package`: `zxy-panel-v0.7.8-stable-engineering.zip`
- `tag`: `v0.7.8`

开发状态检查（默认模式）允许发布清单落后于源码，仍会校验清单自身的版本、包名和下载地址是否一致：

```bash
node scripts/check-version-consistency.mjs --mode dev
```

发布检查要求清单与源码版本完全一致，并校验实际 ZIP 的 SHA256：

```bash
node scripts/check-version-consistency.mjs --mode release --manifest version.json --package /absolute/path/to/zxy-panel-v0.7.8-stable-engineering.zip
```

构建脚本默认读取 `VERSION`，显式版本参数必须与源码相符。历史 CHANGELOG、旧发布目录、持久化版本元数据和未重新构建的产物不属于源码版本检查范围。发布检查不能代替服务器安装、Agent 或 Xray 功能验收。
