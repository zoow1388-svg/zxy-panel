# ZXY Panel

ZXY Panel 是一个面向跨境业务网络节点管理的控制台，用于统一管理专线模式、中转入口、落地出口、固定出口客户、SOCKS5 路由中转和网络核心状态。

## 当前版本

- 稳定版本：`0.7.6.0-base-stable-agent-xray`
- 发布标签：`v0.7.6.0`
- 推荐安装方式：fast/systemd

## 功能列表

- 跨境业务网络节点管理
- 专线模式客户管理
- 中转入口与落地出口配置
- 固定出口客户保持一客户一入口一出口
- 直连客户与固定出口客户分流管理
- SOCKS5 路由中转
- 客户专属入口与固定出口能力
- 网络策略中心，DNS、IPv6、QUIC、UDP 和 53 端口策略由用户手动配置
- 节点诊断与一键体检中心
- 托管升级中心与远程版本清单
- fast/systemd 一键安装，保留 Docker 兼容模式

## 一键安装

```bash
bash <(curl -Ls https://raw.githubusercontent.com/zoow1388-svg/zxy-panel/main/install.sh)
```

安装脚本会自动写入：

```text
ZXY_UPDATE_MANIFEST_URL=https://raw.githubusercontent.com/zoow1388-svg/zxy-panel/main/version.json
```

## 一键升级

```bash
zxy-panel update
```

也可以重新执行安装入口：

```bash
bash <(curl -Ls https://raw.githubusercontent.com/zoow1388-svg/zxy-panel/main/install.sh)
```

## 常用命令

```bash
zxy-panel info
zxy-panel status
zxy-panel restart
zxy-panel logs
zxy-panel reset-password
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

- `backend/`：后端 API 和系统升级逻辑
- `frontend/`：前端控制台
- `agent/`：节点 Agent 与网络核心状态上报
- `deploy/`：服务器安装、Agent 安装和卸载脚本
- `scripts/`：CLI 管理命令和发布构建脚本
- `version.json`：远程版本清单
- `releases/`：Release 包和 SHA256 校验文件归档

## 安全说明

不要提交或发布以下内容：

- `/etc/zxy-panel/panel.info`
- API Token
- 管理员密码
- 真实服务器密码
- SSH key
- 私钥
- `.env` 里的生产密钥
- 用户真实客户数据
- `/opt/zxy-panel/data/zxy-panel.json` 里的真实线上数据

如需展示配置格式，请使用：

- `example.panel.info`
- `example.env`
- `example.version.json`

## 版本路线

- `V0.7.6.0-base-stable-agent-xray`：基础稳定版，修复 Agent 空闲反复下发配置、客户编辑、WebBasePath 刷新、默认更新清单写入和系统升级版本比较。
- `V0.7.5.9.1-qr-flow-compatibility-fix-agent-xray`：修复 VLESS 分享链接默认强制 flow 的兼容问题。
- `V0.7.5.9-qr-import-compatibility-agent-xray`：优化二维码导入兼容，默认二维码使用 vless:// 单节点链接。
- `V0.7.5.8.1-diagnosis-polish-upgrade-fix-agent-xray`：优化节点体检评分和托管升级任务状态恢复。
- `V0.7.5.8-node-diagnosis-center-agent-xray`：新增节点诊断与一键体检中心。
- `V0.7.5.7`：托管升级中心。
- `V0.7.5.6-fast-install-agent-xray`：fast/systemd 安装加速。
- `V0.7.5.5-network-policy-center-agent-xray`：网络策略中心，默认不启用强阻断。
