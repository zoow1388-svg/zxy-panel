# ZXY Panel V0.7.6.4 Node Diagnosis

V0.7.5.5 基于 V0.7.5.3 稳定版开发，废弃 V0.7.5.4 的默认强阻断思路。

核心原则：升级只增加能力，不自动改变客户现有网络行为。

新增菜单：高级：网络策略。

可配置项包括：DNS 服务器、queryStrategy、fallback、tcp/udp 53 阻断、中国公共 DNS 阻断、QUIC UDP 443 阻断、IPv6 策略、Clash Meta Quad9、sing-box Quad9。

默认模式为兼容稳定模式，不启用 53 阻断、不禁用 fallback、不阻断 QUIC。
