# ZXY Panel

ZXY Panel 是一个面向跨境业务网络节点管理的控制台，用于统一管理专线模式、中转入口、落地出口、固定出口客户、SOCKS5 路由中转、Clash/Mihomo 订阅和网络核心状态。

## 当前版本

- 稳定测试版本：`0.7.7.5-stability-polish-agent-xray`
- 发布标签：`v0.7.7.5`
- 发布时间：`2026-07-03`
- 发布包：`zxy-panel-v0.7.7.5-stability-polish.zip`

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

```bash
bash <(curl -Ls https://raw.githubusercontent.com/zoow1388-svg/zxy-panel/main/install.sh)
```

也可以指定版本清单地址：

```bash
ZXY_UPDATE_MANIFEST_URL=https://raw.githubusercontent.com/zoow1388-svg/zxy-panel/main/version.json \
bash <(curl -Ls https://raw.githubusercontent.com/zoow1388-svg/zxy-panel/main/install.sh)
```

## 一键升级

```bash
zxy-panel update
```

或直接重新执行安装入口，安装脚本会读取远程版本清单并部署当前稳定测试包：

```bash
bash <(curl -Ls https://raw.githubusercontent.com/zoow1388-svg/zxy-panel/main/install.sh)
```

## 常用命令

```bash
zxy-panel info
zxy-panel status
zxy-panel start
zxy-panel stop
zxy-panel restart
zxy-panel logs
zxy-panel doctor
zxy-panel update
zxy-panel uninstall
```

## V0.7.7.5 发布说明

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

全新安装未创建客户和入站前，备份与绑定检查会显示 INFO；创建入站并绑定客户后，正常结果应包含：

```text
Doctor result: PASS
Xray binding consistency - binding(s) verified
```

## Clash Verge / Mihomo 使用提醒

Clash Verge 首页显示的“来源：服务器IP:面板端口”是订阅源地址，不是节点转发端口。真正的节点端口需要到“代理”页面或订阅 YAML 的 `port` 字段查看。

导入 Clash 订阅后，如首页显示“暂无激活的代理节点”或“内核通信错误”，请先：

1. 进入“代理”页面手动选择 `PROXY` 分组里的节点。
2. 重启 Mihomo / Clash 内核。
3. Windows 环境可尝试以管理员身份启动 Clash Verge。
4. 如仍无流量，先关闭 TUN，只开启系统代理测试；确认可用后再开启 TUN。

## 发布一致性说明

`main/version.json` 是一键安装和一键升级读取的远程版本清单。当前主线应保持以下字段一致：

- `latest`: `0.7.7.5-stability-polish-agent-xray`
- `version`: `0.7.7.5`
- `codename`: `stability-polish`
- `package`: `zxy-panel-v0.7.7.5-stability-polish.zip`
- `tag`: `v0.7.7.5`
