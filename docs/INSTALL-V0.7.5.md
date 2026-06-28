# ZXY Panel V0.7.5 安装说明

`V0.7.5` 系列新增系统升级页面、检查更新入口、生成升级命令入口，并预留网络核心升级区域。

稳定安装命令：

```bash
bash <(curl -Ls https://raw.githubusercontent.com/zoow1388-svg/zxy-panel/main/install.sh)
```

安装流程：

1. 检测系统版本，优先支持 Ubuntu 22.04 / Debian 12。
2. 安装 `unzip curl ca-certificates tar gzip`。
3. 读取远程 `version.json`。
4. 下载稳定发布包。
5. 校验 SHA256。
6. 解压到 `/root`。
7. 执行 `deploy/install.sh`。
8. 输出 `zxy-panel info`、`zxy-panel status` 和面板访问地址。

远程版本清单地址：

```text
https://raw.githubusercontent.com/zoow1388-svg/zxy-panel/main/version.json
```
