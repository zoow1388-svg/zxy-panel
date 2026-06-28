-- SPDX-License-Identifier: AGPL-3.0-only
-- V0.2 当前默认使用单文件 JSON 持久化，方便无依赖部署。
-- 该 schema 是 V0.3/V0.4 迁移 SQLite/PostgreSQL 的设计草案。

CREATE TABLE admin_users (
  id TEXT PRIMARY KEY,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'super_admin',
  enabled INTEGER NOT NULL DEFAULT 1,
  last_login_ip TEXT,
  last_login_at DATETIME,
  created_at DATETIME NOT NULL
);

CREATE TABLE servers (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  ip TEXT NOT NULL,
  host TEXT,
  region TEXT,
  provider TEXT,
  status TEXT NOT NULL DEFAULT 'offline',
  agent_token TEXT NOT NULL,
  cpu_usage REAL DEFAULT 0,
  memory_usage REAL DEFAULT 0,
  disk_usage REAL DEFAULT 0,
  upload_total INTEGER DEFAULT 0,
  download_total INTEGER DEFAULT 0,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

CREATE TABLE nodes (
  id TEXT PRIMARY KEY,
  server_id TEXT NOT NULL,
  name TEXT NOT NULL,
  protocol TEXT NOT NULL,
  host TEXT,
  port INTEGER NOT NULL,
  transport TEXT,
  security TEXT,
  sni TEXT,
  path TEXT,
  remark TEXT,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

CREATE TABLE clients (
  id TEXT PRIMARY KEY,
  username TEXT NOT NULL,
  email TEXT,
  uuid TEXT NOT NULL,
  traffic_limit_gb INTEGER DEFAULT 0,
  traffic_used_gb INTEGER DEFAULT 0,
  expire_at DATETIME,
  subscribe_token TEXT NOT NULL UNIQUE,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

CREATE TABLE client_nodes (
  client_id TEXT NOT NULL,
  node_id TEXT NOT NULL,
  PRIMARY KEY (client_id, node_id)
);

CREATE TABLE operation_logs (
  id TEXT PRIMARY KEY,
  actor TEXT,
  action TEXT NOT NULL,
  ip TEXT,
  detail TEXT,
  created_at DATETIME NOT NULL
);
