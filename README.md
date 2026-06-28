# ZXY Panel

ZXY Panel 是一个面向跨境业务网络节点管理的控制台，用于统一管理专线模式、中转入口、落地出口、固定出口客户、SOCKS5 路由中转和网络核心状态。

## 当前版本

- 稳定版本：`0.7.5.5-network-policy-center-agent-xray`
- 发布标签：`v0.7.5.5`
- 发布时间：`2026-06-28`

## 功能列表

- 跨境业务网络节点管理
- 专线模式客户管理
- 中转入口与落地出口配置
- 固定出口客户保持一客户一入口一出口
- 直连客户与固定出口客户分流管理
- SOCKS5 路由中转
- 客户专属入口与固定出口能力
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

或直接重新执行安装入口，安装脚本会读取远程版本清单并部署稳定包：

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
zxy-panel update
zxy-panel uninstall
```

## 目录说明

```text
zxy-panel/
├── README.md
├── CHANGELOG.md
├── install.sh
├── version.json
├── LICENSE_STRATEGY.md
├── docs/
├── releases/
├── backend/
├── frontend/
├── agent/
└── deploy/
```

- `backend/`：后端服务代码
- `frontend/`：前端控制台代码
- `agent/`：节点 Agent 与网络核心版本上报逻辑
- `deploy/`：服务器部署、Agent 安装和卸载脚本
- `docs/`：安装、升级和版本说明
- `releases/`：稳定版本归档与校验文件
- `version.json`：远程版本清单

## 安全说明

不要提交或发布以下内容：

- `/etc/zxy-panel/panel.info`
- API Token
- 管理员密码
- 真实服务器密码
- SSH key
- 私钥
- `.env` 中的生产密钥
- 用户真实客户数据
- `/opt/zxy-panel/data/zxy-panel.json` 中的真实线上数据

如需展示配置格式，请使用：

- `example.panel.info`
- `example.env`
- `example.version.json`

## 版本路线

- `V0.7.4.3-direct-client-agent-xray`：完成直连客户、固定出口客户、SOCKS5 路由中转和客户专属入口稳定验证。
- `V0.7.5-online-updater`：新增系统升级页面、检查更新入口、生成升级命令入口，并预留网络核心升级区域。
- `V0.7.5.1-update-config-agent-xray`：修复默认远程版本清单地址问题，未配置时显示未配置，网络核心版本优先读取 Agent 上报版本。
- `V0.7.5.2-install-optimized-agent-xray`：优化一键安装速度，修复安装完成后访问地址缺少 WebBasePath 的问题，自动写入 `ZXY_UPDATE_MANIFEST_URL`，优化 docker-compose 兼容和安装提示。
- `V0.7.5.3-ca-cert-fix-agent-xray`：修复后端 API 容器缺少 ca-certificates 导致系统升级页无法通过 HTTPS 读取远程版本清单的问题。
- `V0.7.5.4-dns-stability-agent-xray`：增强服务端 Xray DNS 稳定策略，固定公共 DNS，启用 UseIPv4，禁用 DNS fallback，并增加 DNS 请求阻断规则。
- `V0.7.5.5-network-policy-center-agent-xray`：新增“高级：网络策略”，DNS、IPv6、QUIC、UDP、53 端口阻断和中国公共 DNS 阻断改为用户手动配置，默认不启用强阻断。
- 后续版本：继续完善自动升级、Agent 管理、版本回滚和部署诊断能力。
