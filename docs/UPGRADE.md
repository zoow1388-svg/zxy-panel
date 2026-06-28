# ZXY Panel 升级说明

## 推荐升级命令

```bash
zxy-panel update
```

也可以执行安装入口完成稳定版本更新：

```bash
bash <(curl -Ls https://raw.githubusercontent.com/zoow1388-svg/zxy-panel/main/install.sh)
```

## 系统升级页配置

后台系统升级页应配置：

```text
ZXY_UPDATE_MANIFEST_URL=https://raw.githubusercontent.com/zoow1388-svg/zxy-panel/main/version.json
```

如果未配置远程版本清单，系统升级页应显示“未配置”，不要默认写入错误地址。

## 升级前检查

- 备份 `/etc/zxy-panel`。
- 备份 `/opt/zxy-panel/data`。
- 确认当前版本不低于 `0.7.4`。
- 确认 `version.json` 中的 SHA256 与发布包一致。
- 确认固定出口客户、直连客户、SOCKS5 路由中转配置未被覆盖。

## 回滚建议

保留每次发布的 ZIP 与 `SHA256SUMS`。如果升级异常，可解压上一版本发布包并重新执行：

```bash
cd /root/zxy-panel-<version>
bash deploy/install.sh
```
