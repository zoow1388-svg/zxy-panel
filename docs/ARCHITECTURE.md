# 系统架构 V0.2

## 总体结构

```text
用户浏览器
  ↓
前端管理后台 Vue
  ↓
后端 API 服务 Go
  ↓
单文件数据存储 data/zxy-panel.json
  ↓
Agent 心跳接口
  ↓
节点服务器 Agent
```

## 后端模块

```text
backend
├── api           HTTP 路由、登录、CRUD、订阅、Agent
├── model         数据模型
├── security      密码哈希、JWT
├── store         单文件持久化
└── xray          Xray 配置模板生成
```

## Agent 模块

```text
agent
├── heartbeat     心跳上报
├── monitor       指标采集预留
├── executor      指令执行预留
└── updater       Agent 自更新预留
```

## 权限模型

V0.2 暂时只有超级管理员。后续版本会加入：

- 超级管理员
- 服务器管理员
- 普通客户
- 代理商 / 租户

## 数据流

```text
管理员登录
  ↓
创建服务器
  ↓
创建节点
  ↓
创建客户并绑定节点
  ↓
客户通过订阅 token 获取节点配置
  ↓
Agent 上报服务器状态
```
