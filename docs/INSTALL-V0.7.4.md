# ZXY Panel V0.7.4 安装说明

`V0.7.4.3-direct-client-agent-xray` 已验证以下能力：

- 直连客户。
- 固定出口客户。
- SOCKS5 路由中转。
- 客户专属入口。
- 固定出口 IP 不乱跳。

推荐在新服务器上使用统一安装入口：

```bash
bash <(curl -Ls https://raw.githubusercontent.com/zoow1388-svg/zxy-panel/main/install.sh)
```

如果需要保留 V0.7.4 系列，请在发布包和版本清单中指定对应版本。

安装前请确认：

- 使用 Ubuntu 22.04 或 Debian 12。
- 服务器安全组已放行面板端口和业务端口。
- 不把真实客户数据、管理员密码或 API Token 放进发布包。
