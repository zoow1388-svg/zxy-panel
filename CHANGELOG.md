# Changelog

## V0.7.8 stable-engineering (unreleased)

- Use `VERSION` as the source version baseline and synchronize runtime version labels.
- Distinguish the source development version from the latest published manifest.
- Check source versions before building; check the release manifest and package SHA256 after building.
- Preserve the existing BBR work without changing Xray, Reality, persistence logic, or Agent protocol fields.

## V0.7.7.6 bbr-optimization (unreleased)

- Add host-level BBR detection, enable, and disable controls through the systemd Agent.
- Enable BBR by default after installation when the kernel supports it; failures do not interrupt installation.
- Preserve an administrator disable choice across later upgrades.
- Add the Server Optimization page and BBR status reporting for local and remote Agents.

## V0.7.7.5 stability-polish

- 统一 README、CHANGELOG、构建脚本、前端文案、后端版本、Agent 版本和安装脚本版本，减少发布包与远程 manifest 不一致问题。
- `scripts/build-fast-release.sh` 改为动态 VERSION / CODENAME，默认打包 `0.7.7.5` / `stability-polish`。
- Release 包不再内置带 SHA256 的 `version.json`，避免发布包内部版本清单与外部 release manifest 互相引用导致 hash 不一致。
- Fresh install 没有备份、尚未创建客户绑定时，`zxy-panel doctor` 改为 INFO 提示，不再误报 warning。
- 客户分享弹窗补充 Clash Verge / Mihomo 使用提示，明确订阅源端口不是节点端口、导入后需在代理页选节点、内核通信错误时应重启内核或客户端进程。
- Clash YAML 顶部增加客户端使用说明，减少“导入成功但当前节点为空”的误判。
- 保留 V0.7.7.3 Clash/Mihomo 订阅绑定修复和 V0.7.7.2 BindingCheck / Agent apply 校验 / Doctor 深度检测。
- 不修改 Xray 配置生成核心规则，不修改客户数据结构，不清空用户数据。

## V0.7.7.4 ui-version-sync-polish

- 修复仪表盘与前端页面仍显示 V0.7.7.2 / 旧版本号的问题。
- 统一前端 `APP_VERSION`、Dashboard、客户管理、入站管理、网络策略、系统升级等页面版本文案为 V0.7.7.4。
- 统一后端 `/api/health`、系统信息、默认数据版本和 Agent 版本为 `0.7.7.4-ui-version-sync-polish-agent-xray`。
- 重新构建 `frontend/dist`，避免浏览器加载旧打包资源后仍显示旧版本。
- 保留 V0.7.7.3 Clash 订阅绑定修复：多客户多端口订阅不再混淆。
- 保留 V0.7.7.2 端口 UUID BindingCheck、Agent apply 校验和 Doctor 深度检测。
- 不修改 Xray 配置生成、Clash 订阅核心逻辑、客户管理核心逻辑和用户数据。

## V0.7.7.3 clash-subscription-bind-fix

- 修复 Clash/Mihomo 订阅在多客户、多入站、多端口场景下容易显示或合并为同一端口的问题。
- Clash YAML 代理名称增加客户名与端口信息，避免同名节点在客户端中被覆盖。
- Clash 订阅输出按端口排序，减少导入后顺序跳动。
- Clash 订阅支持 `node_id` / `node` / `entry_id` 参数，便于后续生成指定入站单节点订阅。
- 保留 V0.7.7.2 的 BindingCheck、Agent apply 校验和 Doctor 深度检测。

## V0.7.7.2 client-sync-fix

- 新增 `backend/internal/configplan`，将面板期望端口、客户 UUID、SOCKS5 账号绑定抽象为统一校验计划。
- `/api/agent/sync` 下发前增加 BindingCheck，避免端口存在但客户 UUID 未写入 Xray 配置。
- Agent 写入临时配置、替换正式配置、重启 Xray 后均进行一致性校验。
- Doctor 增加 Xray binding consistency 检查，能识别客户、入站、Xray 实际配置是否一致。
- 客户更新改为 merge update，避免前端局部提交误清空 `node_ids` / `relay_route_ids` / `enabled` 等关键字段。
- 保留 V0.7.7.1 Clash/Mihomo 订阅能力。

## V0.7.7.1 clash-import-polish

- 新增 `zxy-panel backup` 一键备份命令，集中备份数据、面板信息、环境变量、Nginx 配置、Xray 配置和关键 systemd 配置。
- 新增 `zxy-panel backup-list` 备份列表命令，便于快速查看最近备份文件。
- 新增 `zxy-panel restore [backup-file]` 恢复命令，恢复后自动 daemon-reload、重启 API/Agent/Xray、reload Nginx 并执行 doctor。
- 安装/升级前自动创建 pre-install 备份；全新安装没有旧数据时自动跳过。
- `zxy-panel doctor` 增加备份状态和数据文件检查。
- `zxy-panel info` 默认隐藏 Password 与 API Token，使用 `--show-secret` 才显示完整敏感信息。
- 优化一键安装速度和安装体验。
- 外层 `install.sh` 默认启用 `DEBIAN_FRONTEND=noninteractive`、`NEEDRESTART_MODE=a`、`NEEDRESTART_SUSPEND=1`，减少依赖安装阶段卡住。
- 已安装基础依赖时跳过安装，避免重复 apt update 和重复安装。
- 已安装 Xray-core 时默认跳过重新下载，重复安装明显更快。
- 新增 `ZXY_FORCE_INSTALL_XRAY=1` 强制重装 Xray-core。
- 新增 `ZXY_SKIP_XRAY_INSTALL=1` 跳过 Xray-core 安装。
- 安装日志增加阶段提示，继续保留安装后 `zxy-panel doctor` 自检。
- 不修改客户管理、入站、中转、落地出口、二维码、网络策略、Agent apply 等核心逻辑。

## V0.7.5.5 network-policy-center

- 新增独立菜单“高级：网络策略”。
- DNS、IPv6、QUIC、UDP、53 端口阻断和中国公共 DNS 阻断改为用户可选配置。
- 升级默认保持兼容稳定模式，不自动启用强阻断，不覆盖现网策略。
- 提供兼容稳定模式、公共 DNS 稳定模式、DNS 防泄漏增强模式、严格防泄漏模式和自定义模式。
- 应用网络策略前支持预览，应用时备份上一次策略，并支持一键回滚。
- 系统检测只提示 DNS 与网络策略风险，不自动修改配置。
- 保留 V0.7.5.3 CA 证书修复和 V0.7.5.2 一键安装优化，不再采用 V0.7.5.4 的默认 DNS 强阻断策略。
