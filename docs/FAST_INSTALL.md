# ZXY Panel V0.7.6.1 Node Diagnosis Patch

## 目标

把默认安装从“客户服务器现场 Docker build / Go build / Node build”改成“预构建产物直接运行”。

优化后首次安装目标：

- 干净 Ubuntu：1-3 分钟
- 已有 nginx / ca-certificates / python3：30-60 秒
- 升级补丁：10-30 秒

## 改动点

1. `deploy/install.sh`
   - 新增 `ZXY_INSTALL_MODE=auto|fast|docker`
   - 检测到 `bin/zxy-panel-api-linux-amd64`、`bin/zxy-agent-linux-amd64`、`frontend/dist/index.html` 时自动进入 fast 模式
   - fast 模式跳过 Docker、docker-compose、Go 镜像、Node 镜像、npm 现场构建
   - Nginx 直接托管前端 dist 并反代 API/sub/s
   - 缺少预构建产物时自动回退 docker 兼容模式

2. `deploy/agent-install.sh`
   - Agent 优先使用内置二进制
   - Xray 优先使用内置 `bin/xray-linux-amd64`
   - 没有内置 Xray 时才走官方脚本下载

3. `scripts/zxy-panel`
   - 支持 fast/systemd 模式的 `status`、`restart`、`logs`、`reset-password`、`uninstall`
   - 兼容旧 Docker 模式

4. `scripts/build-fast-release.sh`
   - 自动构建后端 API、Agent、前端 dist
   - 自动打包 `zxy-panel-v版本-node-diagnosis-center.zip`
   - 自动输出 SHA256 和 `version.fast.json`

5. `.github/workflows/build-fast-release.yml`
   - 支持 GitHub Actions 手动构建 fast release artifact
   - 支持 tag 发布时自动生成 GitHub Release 资产

## 使用方式

把本补丁包里的文件覆盖到仓库根目录后执行：

```bash
bash scripts/build-fast-release.sh 0.7.5.8
```

构建完成后会生成：

```text
dist-release/zxy-panel-v0.7.6.1-node-diagnosis-center.zip
dist-release/version.fast.json
```

把 zip 上传到 GitHub Release，再把 `version.fast.json` 中的 `download_url` 替换成真实 Release 下载地址，并更新主 `version.json`。

## 临时强制 fast 模式测试

```bash
ZXY_INSTALL_MODE=fast bash deploy/install.sh
```

如果缺少预构建产物，fast 模式会直接报错；auto 模式会自动回退 Docker。

## 回退 Docker 兼容模式

```bash
ZXY_INSTALL_MODE=docker bash deploy/install.sh
```
