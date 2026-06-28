# CHANGELOG

## V0.7.5.3 ca-cert-fix

- 修复后端 API 容器缺少 ca-certificates 导致系统升级页无法通过 HTTPS 读取 GitHub raw `version.json` 的问题。
- `backend/Dockerfile` 增加 ca-certificates 和 update-ca-certificates。
- 检查更新失败时返回更具体的错误信息，便于区分 DNS、TLS、HTTP 状态码和 JSON 解析问题。
- 保留 V0.7.5.2 的一键安装速度优化、完整访问地址输出、`ZXY_UPDATE_MANIFEST_URL` 自动写入和 docker-compose 回退逻辑。

## V0.7.5.2 install-optimized

- 优化一键安装速度，减少重复依赖安装和重复更新软件源。
- 修复安装完成后访问地址缺少 WebBasePath 的问题。
- 自动写入 `ZXY_UPDATE_MANIFEST_URL`，系统升级页不再显示远程版本清单未配置。
- 优化 docker-compose 兼容和安装提示。
- 保留 V0.7.5.1 已验证的系统升级配置、直连客户、固定出口客户和 SOCKS5 路由中转能力。

## V0.7.5.1 update-config

- 修复默认 `version.json` 地址写死问题。
- 未配置远程版本清单时显示未配置。
- 网络核心版本优先读取 Agent 上报。
- 优化系统升级页提示。

## V0.7.5 online-updater

- 新增系统升级页面。
- 新增检查更新入口。
- 新增生成升级命令入口。
- 预留网络核心升级区域。

## V0.7.4.3 direct-client

- 新增直连客户入口。
- 客户管理支持直连客户 / 固定出口客户。
- 固定出口客户保持一客户一入口一出口。
- 修复客户分享复制普通入站的问题。
