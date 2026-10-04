# ZXY Panel fast/systemd 安装

## 范围与状态

源码 `0.7.8-stable-engineering`，候选未发布，尚缺可销毁测试机的安装/升级/完整快照验收。
目标为 Ubuntu 22.04 / Debian 12、Linux amd64、systemd。旧系统、ARM 和容器内冒充宿主机不在本轮范围。
公开一键安装读取正式清单，不安装本地候选；隔离通过不构成安装耗时承诺。

## 产物与服务

- API：bin/zxy-panel-api-linux-amd64，systemd zxy-panel-api。
- Agent：bin/zxy-agent-linux-amd64，可选宿主机服务 zxy-agent。
- 前端：当前源码构建的 frontend/dist，宿主机 Nginx 托管，不启动前端容器。
- 默认根目录 /opt/zxy-panel、配置 /etc/zxy-panel，支持经过验证的显式/既有目录。
- 默认 DB data/zxy-panel.json；fast 保留合法真实指针，Docker 不新增外部 DB 映射。
- API 默认回环 127.0.0.1:8088，实际端口从配置读取。面板端口与 WebBasePath 沿用旧值或新装随机生成。
- API/sub/s 根路径和随机前缀均支持，查询参数保留。assets 和 Vue history 刷新必须在真实 Nginx 验证。

## 构建

只在独立可写源码副本构建；脚本会更新该副本 bin/dist 并重建 dist-release，不能在冻结工作树执行。

```bash
bash scripts/build-fast-release.sh 0.7.8 stable-engineering
```

读取 VERSION 并拒绝版本不一致，构建 Linux amd64 API/Agent 和前端，生成 ZIP/SHA256/外部 version.fast.json 模板。
冻结 frontend/dist 不代表新源码产物，必须核对生成资产和源码提交。正式 download_url/SHA256 仅在发布获批后更新。

## 测试机安装

以下只供已授权、可恢复完整快照的临时测试机，不在开发宿主机或当前 WSL 运行。

```bash
ZXY_INSTALL_MODE=fast bash deploy/install.sh
```

auto 仅在新装缺预构建产物时回退 Docker；显式 fast 缺产物提前拒绝。
既有模式须明确，歧义或跨模式请求提前拒绝。fast 不默认安装 Docker，不现场构建前后端。
依赖/Xray 已存在时复用，缺失才走安装流程；没有实际测速前不宣称安装秒数。
AUTO_AGENT=false 不安装/改写其配置，不擅自停止已有 Agent/Xray；已有 Agent 须保持合法身份和运行参数。
FRESH_INSTALL=true 会清理经过匹配备份验证的正式 DB，并受既有 Agent 身份护栏限制；不是常规升级。

## Docker 兼容入口

```bash
ZXY_INSTALL_MODE=docker bash deploy/install.sh
```

仅同模式或干净新装，保留原 Compose 项目、回环监听和 app/data 绑定。旧 Docker 现场构建仍可能耗时较长。
容器名不等于归属，须确认本地 context、项目标签、配置/工作目录和数据挂载。
Docker 配置/数据恢复另要求 Compose 能输出 JSON 配置并支持 `up --pull never`；缺少能力时拒绝恢复，不代表不能使用原安装入口。
安装完成前须确认 API health JSON 的 service/status/version 和 doctor。PASS 不代替 Agent/Xray/客户联网、完整绑定核验或回滚。
失败必须非零并保留恢复证据。升级、配置/数据恢复与整版回滚见 [UPGRADE.md](UPGRADE.md)。
