# API 草案 V0.2

## Health

```http
GET /api/health
```

## Auth

```http
POST /api/auth/login
```

Body:

```json
{"username":"安装输出的账号","password":"安装输出的密码"}
```

返回 JWT token。除 health、login、subscription、agent heartbeat 外，后台接口需要：

```http
Authorization: Bearer <token>
```

## Dashboard

```http
GET /api/dashboard
```

## Servers

```http
GET /api/servers
POST /api/servers
GET /api/servers/{id}
PUT /api/servers/{id}
DELETE /api/servers/{id}
```

## Nodes

```http
GET /api/nodes
POST /api/nodes
GET /api/nodes/{id}
PUT /api/nodes/{id}
DELETE /api/nodes/{id}
GET /api/nodes/{id}/xray-config
```

## Clients

```http
GET /api/clients
POST /api/clients
GET /api/clients/{id}
PUT /api/clients/{id}
DELETE /api/clients/{id}
POST /api/clients/{id}/reset-token
```

## Subscription

```http
GET /sub/{token}
```

V0.2 返回 VLESS 订阅文本。V0.4 会继续支持多客户端格式。

## Agent

```http
POST /api/agent/heartbeat
```

Header:

```http
X-Agent-Token: <server.agent_token>
```

## Logs

```http
GET /api/logs
```
