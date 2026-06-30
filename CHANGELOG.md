# CHANGELOG

## V0.7.6.1 zip-path-install-fix

- 修复安装完成后 `/opt/zxy-panel/.env` 未默认写入 `ZXY_UPDATE_MANIFEST_URL` 的问题。
- 安装脚本会更新或追加 `ZXY_UPDATE_MANIFEST_URL=https://raw.githubusercontent.com/zoow1388-svg/zxy-panel/main/version.json`，并保留 `.env` 其它已有配置。
- 修复系统升级中心版本比较逻辑，改为按数字版本段比较。
- `0.7.6.1` 会正确大于 `0.7.5.9.1`，远程版本低于或等于当前版本时显示“当前已是最新版本”。
- 禁止系统升级页为旧版本生成降级命令，托管升级任务也不会把旧版本当成新版本。
- 修复 Agent 空闲状态反复下发配置导致 Xray 周期性重启的问题。
- 修复客户管理编辑按钮，可以修改客户名称、禁用客户、启用客户。
- 修复 WebBasePath 下前端页面 F5 刷新空白问题。
- 保留 V0.7.5.9.1 的 vless:// 单节点二维码、flow 兼容修复、HTTP 订阅分离和二维码下载能力。
- 保留 fast/systemd 安装、网络策略中心、节点体检中心和托管升级中心。

## V0.7.5.9.1 qr-flow-compatibility-fix

- 修复 V0.7.5.9 默认 VLESS 分享链接强制添加 `flow=xtls-rprx-vision` 导致节点无法连接的问题。
- 当前服务端 `client.flow` 为空时，分享链接和二维码都不带 `flow`。
- 默认二维码继续保持 `vless://` 单节点链接，不回退到 HTTP 短分享地址。
- 默认通用二维码不强制加入 `packetEncoding=xudp`。

## V0.7.5.9 qr-import-compatibility

- 修复客户分享弹窗中二维码内容错误的问题。
- V2rayN / Shadowrocket 默认二维码编码 `vless://` 单节点链接。
- 订阅二维码和单节点二维码分离。
- 二维码改为本地生成，并增加下载二维码图片按钮。

## V0.7.5.8.1 diagnosis-polish-upgrade-fix

- 节点体检中心优化评分逻辑。
- DNS/IPv6 注意项轻扣分。
- 区分宿主机 DNS、Xray DNS 和客户端浏览器 DNS。
- 托管升级改为独立 systemd runner。
- 新增卡死升级任务识别和清理入口。

## V0.7.5.8 node-diagnosis-center

- 新增节点诊断与一键体检中心。
- 检测 API、Agent、Xray、Nginx、面板端口和 WebBasePath。
- 支持生成并复制诊断报告。

## V0.7.5.6 fast-install

- 默认安装模式改为 fast/systemd。
- 使用预编译后端、预编译 Agent 和预构建前端 dist。
- Nginx 直接托管前端静态文件并反代 API、订阅和短链。
- 保留 Docker 兼容模式。

## V0.7.5.5 network-policy-center

- 新增“高级：网络策略”。
- DNS、IPv6、QUIC、UDP、53 端口阻断和中国公共 DNS 阻断改为用户可选配置。
- 默认不启用强阻断，不覆盖现网策略。

## V0.7.5.3 ca-cert-fix

- 修复后端 API 容器缺少 ca-certificates 导致系统升级页无法通过 HTTPS 读取 GitHub raw `version.json` 的问题。

## V0.7.5.2 install-optimized

- 优化一键安装速度。
- 修复安装完成后访问地址缺少 WebBasePath 的问题。
- 自动写入 `ZXY_UPDATE_MANIFEST_URL`。

## V0.7.5.1 update-config

- 修复默认 `version.json` 地址写死问题。
- 未配置远程版本清单时显示未配置。
- 网络核心版本优先读取 Agent 上报。

## V0.7.4.3 direct-client

- 新增直连客户入口。
- 客户管理支持直连客户 / 固定出口客户。
- 固定出口客户保持一客户一入口一出口。
- 修复客户分享复制普通入站的问题。
