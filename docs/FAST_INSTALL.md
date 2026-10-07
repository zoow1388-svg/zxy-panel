# ZXY Panel fast/systemd 安装

## 范围与状态

源码及正式清单版本为 `0.7.8-stable-engineering`，正式标签 `v0.7.8`；保留测试预发布 `v0.7.8-test.1`。实际公开下载与 Latest 状态以 GitHub Release 为准。
已通过真机验收：Ubuntu 22.04、Linux amd64、fast/systemd 的新安装、重复安装、配置/数据恢复、V0.7.7.5 手动和后台托管升级、原客户端联网、重启恢复与 300 秒空闲观察。
Debian 12 是原设计目标，但本轮尚未真机验收；Docker、完整快照恢复和更早升级起点同样未验证。旧系统、ARM 和容器内冒充宿主机不在本轮范围。
公开一键安装读取远端正式清单。实际测试耗时不构成其它线路或服务器的速度承诺。详见 [V0.7.8 发布说明](releases/v0.7.8.md)。

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
冻结 frontend/dist 不代表新源码产物，必须核对生成资产和源码提交。此次复用已有 64 场景浏览器验收对应的 HTML/JS/CSS 和已验证二进制，包内哈希与来源清单一致；未重新构建。
version.json 使用正式 v0.7.8 的 download_url 和原包 SHA256；发布流程必须先下载核验附件，再投放 main 清单。不要仅凭本地清单内容假定远端下载可用。

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

以下保留既有兼容机制说明，不表示 Docker 已通过本轮真机验收，也不是 fast 与 Docker 互相迁移的授权。

```bash
ZXY_INSTALL_MODE=docker bash deploy/install.sh
```

仅同模式或干净新装，保留原 Compose 项目、回环监听和 app/data 绑定。旧 Docker 现场构建仍可能耗时较长。
容器名不等于归属，须确认本地 context、项目标签、配置/工作目录和数据挂载。
Docker 配置/数据恢复另要求 Compose 能输出 JSON 配置并支持 `up --pull never`；缺少能力时拒绝恢复，不代表不能使用原安装入口。
安装完成前须确认 API health JSON 的 service/status/version 和 doctor。PASS 不代替 Agent/Xray/客户联网、完整绑定核验或回滚。
失败必须非零并保留恢复证据。升级、配置/数据恢复与整版回滚见 [UPGRADE.md](UPGRADE.md)。

## 已知问题

- BBR JSON 实际为 `"enabled":true` 时，安装器因按带空格文本匹配，可能显示“未支持或仍未开启”。测试输出显示实际内核为 bbr/fq；不能用错误文案判断真实状态。此次不修改安装器或测试包。
- 有过客户端网络波动报告；已检查入口监听和服务状态，原因仍未确认，不宣称与升级有关或无关。
- 移动端 Relay 表格溢出、Token 重置及跨进程写入边界等既有问题不在本轮整改范围。
