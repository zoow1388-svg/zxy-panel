# Server Identity 修复方案

## 1. 文档状态与范围

- 状态：设计草案，等待产品负责人确认，不代表功能已实现。
- 设计基线：当前工作区 `VERSION` 为 `0.7.8-stable-engineering`；已提交 HEAD 为 `9b26fe23e832e9bfeff82405c0ecfb303856ec41`。分析包含当前未提交的 BBR 改动，不能用已提交版本代替工作区源码。
- 本次交付只有本文件。不修改源码、数据库、Xray、Agent 协议、安装配置或生产服务；不提交、不发布。
- 下文的字段、管理接口、安装步骤和迁移工具均为未来实现建议，不是当前可执行功能。
- 现有 `server_id`、`X-Agent-Token`、心跳和同步 JSON 保持不变。InstallID 由安装与管理侧维护，不增加到 Agent 心跳或同步协议。

目标是分开三件事：服务器记录标识、安装实例标识、认证凭据。版本号和地址只能用于展示、兼容性提示与诊断，不能决定服务器身份、归属或删除。

## 2. 当前数据模型

### 2.1 Server 与引用关系

当前模型位于 `backend/internal/model/model.go`，数据通过 `backend/internal/store/store.go` 保存到单个 JSON 文件。

| 字段或关系 | 当前职责 | 身份方面的限制 |
| --- | --- | --- |
| `Server.ID` | 面板内服务器记录主键；也是 Agent 请求的 `server_id` | 随创建记录生成，不是宿主机的安装实例标识 |
| `Server.AgentToken` | 指定 Server 的认证凭据 | 是秘密，可以轮换；相同 Token 也不能单独证明同一物理服务器 |
| `Server.IP` / `Host` | 地址与连接展示 | 可变，可能共享，不能用于唯一身份 |
| `Server.AgentVersion` | Agent 上报的软件版本 | 当前被 `pickLocalServer()` 用作保留候选的条件 |
| `Server.XrayVersion` / `ConfigHash` | 网络核心版本、配置状态 | 不是身份字段 |
| `Server.Status` / `LastSyncAt` | 运行状态、同步时间 | 在线或较新不等于拥有其他记录的资源 |
| `PanelData.Version` | 数据中的产品版本元数据 | 当前会在 normalize 中改写，不是迁移版本或身份版本 |
| `Node.ServerID` | 入站归属哪个 Server | 合并时当前会改写 |
| `RelayRoute.RelayServerID` | 中转入口归属哪个 Server | 合并删除和普通删除均缺少完整引用保护 |
| `RelayRoute.LandingNodeID` | 面板内落地入站引用，具体约束取决于路由模式 | 通过 Node 间接依赖 Server；不能只检查中转入口 |
| `Client.NodeIDs` / `RelayRouteIDs` | 客户引用入站和中转线路 | 没有持久化的直接 ServerID，但存在间接依赖 |
| `Server.BBRPendingAction` | 发给指定服务器的待执行 BBR 操作 | 合并或删除时不能静默转移、丢弃操作 |

`LandingExit` 是独立出口地址记录，没有 ServerID 字段；本方案不改变它的模型或出口逻辑。客户创建请求中的 `relay_server_id` 会用于构造中转记录，不等同于 Client 模型新增 ServerID。

```text
Server.ID <--- Node.ServerID <--- Client.NodeIDs
    ^              ^
    |              +--- RelayRoute.LandingNodeID
    +--- RelayRoute.RelayServerID <--- Client.RelayRouteIDs
```

### 2.2 当前安装、注册与心跳调用链

当前没有独立的 Agent 注册 HTTP 接口。实际流程是预先建立服务器记录，再把 ID/Token 配置给 Agent：

1. API 启动调用 `Store.Open()`，加载数据并执行 `ensureSingleModeLocalServer()`。
2. 本机 Server 可以由 store 初始化；管理员也可以通过受保护的 `POST /api/servers` 创建记录。
3. `deploy/install.sh` 的 `install_local_agent()` 直接读取 JSON，按名称、IP/Host、在线状态评分选择 Server。
4. `deploy/agent-install.sh` 将 `ZXY_SERVER_ID` 和 `ZXY_AGENT_TOKEN` 写到 `/etc/zxy-panel/agent.env`。
5. Agent 读取这组配置，通过 `POST /api/agent/sync` 和 `POST /api/agent/heartbeat` 发请求。
6. 后端 `validateAgentToken()` 按 ServerID 查记录、按该记录的 Token 认证；同步按同一 ServerID 筛选 Nodes 和 RelayRoutes。

因此，仅修 store 的版本字符串不足以解决根因。后续实施还必须处理安装器的地址评分、本机默认 Server 选择及写入边界，但本次不改这些代码。

### 2.3 已确认的风险与证据

2026-10-02 已在项目外复制源码并使用合成 JSON 验证，无生产数据库、无网络监听、无服务启动。

| 案例 | 已观察到的行为 | 根本问题 |
| --- | --- | --- |
| A：不同版本 | 在线 `0.7.8` 与离线 `0.7.7.6` 调换候选顺序会改变保留结果 | 硬编码版本优先与其他排序条件不构成稳定规则；候选来自无固定顺序的 map |
| B：同版本、不同身份 | 同地址但不同 ID/Token 的两条记录合并成一条；节点归属改变；被删除身份认证返回 404 | 地址相同被当成足以删除身份的依据 |
| B 对照 | 不同非回环地址的两条记录均保留 | 不是版本相同就一定合并，风险有候选地址匹配的前提 |
| C：自动合并删除 | Node.ServerID 被迁移，RelayServerID 保留旧值并写入 JSON | 引用迁移不完整 |
| C：普通删除 | 只有中转引用而无自身入站的 Server 可被删除，接口返回成功 | 删除只检查 Nodes，没有检查 RelayRoutes |

证据目录：`C:/Users/52210/Documents/zxy-panel-merge-validation/20261002-074411/`。新增 6 项验证中 5 项安全断言失败、1 项对照通过；原有 2 项版本比较测试通过。失败是风险复现，不是修复验收通过。该结果未证明 Linux 真机或客户端连通行为。

## 3. 新数据模型

### 3.1 推荐的最小增量

保留所有现有主键和业务字段，只建议增加以下持久化字段：

| 所属对象 | 建议字段 | 含义与约束 |
| --- | --- | --- |
| `Server` | `InstallID`，JSON 为 `install_id`，可缺省 | 安装实例的随机 UUID；非空值在当前面板内唯一；普通编辑不能替换 |
| `PanelData` | `IdentitySchema`，JSON 为 `identity_schema` | 缺省或 0 表示历史数据；1 表示支持本方案的增量格式。与产品 VERSION 无关 |
| `PanelData` | `LocalServerID`，JSON 为 `local_server_id`，可缺省 | 经明确登记的本机 Server.ID；非空时必须引用存在的 Server |

不新增一套数据库，不把 InstallID 用作 Node、RelayRoute 的外键，不批量替换 Server.ID。身份状态由字段和校验结果派生，避免重复保存容易失配的状态字段：

- `legacy_unbound`：InstallID 为空，仍按原 ID/Token 工作，但不能自动合并或按地址认领。
- `bound`：InstallID 非空、唯一且绑定有效。
- `conflict`：发现重复 InstallID、绑定不一致或缺失引用。报告冲突，不自动修复。

```text
InstallID --登记绑定--> Server.ID --认证--> 对应 AgentToken
                           |
                           +--既有 Nodes / RelayRoutes 引用，保持不变

AgentVersion / XrayVersion / IP / Host 不在身份映射路径中
```

### 3.2 必须成立的约束

1. AgentVersion 变化不得创建、删除、合并 Server 或改变资源归属。
2. 地址相同、名称相同、Token 相同都不能单独触发自动合并。
3. 合法重复注册只返回已绑定的 Server，不删除另一条记录。
4. 非空 InstallID 必须唯一；出现重复是冲突，不是自动去重依据。
5. Nodes 引用的 Server 必须存在；RelayRoutes 引用的中转 Server 必须存在。面板落地 Node 的要求沿用现有路由模式约束，不为手工 SOCKS5 落地强造 Node。
6. Client 的入站与线路引用、LocalServerID 必须保持有效；禁用资源也不能忽略引用。
7. 任何失败不得返回写入成功，不得只改内存而留下与磁盘不同的正式状态。
8. `GET`、普通数据加载和版本归一化不得进行 Server 合并或数据迁移。
9. 身份字段只能由专门的管理操作维护；普通 Server PUT 不能凭请求缺字段清空 InstallID 或替换 ID。

InstallID 标识的是安装实例，不是物理机器硬件 ID，也不是认证凭据。更换地址、升级软件、切换 fast/Docker 不能改变它。

## 4. InstallID 生成流程

### 4.1 宿主机持久化

建议安装器在宿主机维护 `/etc/zxy-panel/install-identity.json`：

```json
{
  "format_version": 1,
  "install_id": "<random-uuid-v4>",
  "created_at": "<utc-timestamp>"
}
```

这是结构示意，不是可直接使用的身份文件。文件不得含 AgentToken 或管理员密码。

1. 安装前检查已有身份文件、`agent.env`、安装模式及对应的 Server 配置，先备份，不启动业务数据推断合并。
2. 已存在合法身份文件时原样复用；文件格式损坏或与已登记身份冲突时停止登记，不能悄悄生成替代 ID。
3. 真正的新安装使用操作系统密码学随机源生成 UUID，不能从 IP、主机名、MAC、版本号、时间戳或 AgentToken 推导。
4. 并发安装使用安装级排他锁；临时文件在同目录写入，设置 root 所有、0600，完成校验和同步后原子发布，第二个安装进程复用已提交结果。
5. 安装器已有 Python 3 依赖时可使用兼容 Python 3.5 的标准库随机能力；不能重新引入 `import secrets`，也不要求客户机安装 Go 来生成 ID。
6. 登记成功后 Agent 仍只读取现有 `agent.env`；身份文件不需要 Agent 读取或上报。
7. fast 和 Docker 使用同一个宿主机身份文件与持久化数据；容器重建不得生成新身份。

Release ZIP、源码仓库和系统镜像模板不得内置真实身份文件、agent.env、Token 或线上数据。备份可以包含身份文件，但应作为受保护的私有运维数据，不上传公共仓库。

### 4.2 生命周期边界

| 操作 | InstallID 与 ServerID 的处理 |
| --- | --- |
| 重复安装、版本升级、服务重启 | 复用，不因失败重试而轮换 Token |
| 更换公网 IP、域名、WebBasePath | 身份不变，地址展示与身份分离 |
| fast 与 Docker 切换 | 复用同一宿主机身份及 ID/Token |
| 合法的整机备份恢复，原实例已经停止 | 恢复原身份与对应 ID/Token，确认没有双实例运行 |
| 克隆成第二个独立节点 | 启动前经明确授权重建 InstallID，并登记新的 ServerID/Token；不能继承原客户归属 |
| `FRESH_INSTALL=true` | 清业务数据与重建身份是两个不同决策，必须分别确认；不能借清数据隐式认领或替换远程 Agent 身份 |
| 身份文件丢失，但 agent.env 仍存在 | 按历史绑定恢复流程处理，不当作全新机器自动注册 |
| 已登记的 Server 被删除 | 原 Agent 凭据失效；重复安装不能自动复活已删除记录，需管理员明确重新登记 |

身份重建必须先记录旧映射、检查引用和备份。普通卸载不能默认删除身份文件并在重装时重新认领旧资源；彻底清除身份是另一个需要确认的运维操作。

## 5. Agent 注册流程

### 5.1 保持协议，新增管理侧登记能力

推荐未来新增受管理员认证保护的管理接口，例如 `POST /api/servers/register-installation`。它不是当前已经存在的接口，也不是 Agent 直接调用的注册协议。

建议管理请求只包含 InstallID、可选的预期 ServerID、是否明确登记为本机及展示信息。历史身份首次绑定还需要核验该 Server 的当前 AgentToken；该凭据只能在受保护请求中使用，不写入请求日志。普通 AgentToken 不能授权管理登记或任意 Server 写操作。

远程管理必须使用可信 TLS 或受保护通道；本机可沿用 loopback。仅因为请求来源看起来是回环地址，不能绕过管理员认证。

```text
宿主机安装器读取/生成 InstallID
  -> 管理员授权的登记操作
  -> 验证预期 ServerID、已有绑定与凭据
  -> 在锁内完成唯一性检查及持久化
  -> 返回稳定 ServerID 和必要的 Agent 配置
  -> 安装器安全写入 agent.env
  -> Agent 按既有协议启动
```

### 5.2 新安装与重复登记

| 条件 | 建议行为 |
| --- | --- |
| 明确的新安装，InstallID 未登记，没有预期的旧 ServerID | 创建一次 Server；绑定 InstallID；按现有安全生成方式分配 ID/Token |
| InstallID 已唯一绑定，预期 ServerID 一致 | 幂等返回同一记录；不换 ID、Token，不迁移业务引用 |
| 已绑定，但请求携带不同预期 ServerID | 返回冲突，所有记录保持不变 |
| 预期 ServerID 已不存在 | 返回未找到，不能因 IP 相同改用其他 Server 或新建代替 |
| 相同 InstallID 已出现于多条记录 | 返回冲突并提供脱敏诊断，不自动挑一条 |
| 旧 Server 无 InstallID，管理员核验原凭据并明确绑定 | 为该记录填 InstallID，保持现有 ServerID/Token 和引用 |
| 已绑定 Server 要改为另一个 InstallID | 普通登记拒绝，必须走单独审批的解绑/重新登记流程 |

已持有管理员权限的安装器可以恢复本次登记结果；仅知道 InstallID 的匿名或 Agent 请求不能取得 Token。自动登记不得把管理员密码长期留在 Agent 配置中。

安装器写入 `agent.env` 失败时不能宣告完成或再创建 Server。下次受授权登记用同一 InstallID 恢复已提交记录，再完成本地配置；因此不用跨 JSON 和宿主机配置文件伪造一个不存在的原子事务。

### 5.3 本机选择规则

- 本机默认记录只由经验证的 `PanelData.LocalServerID` 指定，不再按名称、IP/Host、版本、在线状态评分。
- 本机登记标记只有管理员确认的本机安装操作可写；远程注册不能隐式覆盖它。
- 未登记、指定 ID 缺失或候选有冲突时返回明确错误，不能取 map 的第一项或“在线最多的一台”替代。
- 新面板允许短暂存在空 Server 列表，等待安装器明确登记；读取接口不负责补建或合并。
- 多服务器业务请求应携带明确 ServerID。本方案不修改客户或入站核心逻辑，后续实施只能替换默认身份解析，不能改写显式传入的合法归属。

## 6. 心跳与同步流程

保留当前 URL、请求结构、`X-Agent-Token` 和配置响应。InstallID 不加入心跳和同步 JSON，现有 Agent 不需要修改协议。

1. 检查原有 `server_id` 与 Token；按 ServerID 精确定位记录，不能使用版本号、IP 或 Host 兜底查找。
2. 在身份写操作所用的同一锁边界内重新确认 Server 存在、凭据有效，避免认证后删除与更新交错。
3. 仅更新被认证的同一 Server 的状态、版本、指标及既有 BBR 回执，不生成或重新绑定 InstallID。
4. AgentVersion 变化仅更新软件元数据；版本不符最多提示兼容性，不改变身份和节点归属。
5. 同步仍按既有 ServerID 筛选 Nodes/RelayRoutes，沿用当前 Xray 生成和 config hash/apply 链路。
6. 持久化失败应返回失败并保留一致的正式状态，不能吞错后返回成功。

| 认证情况 | 兼容行为 |
| --- | --- |
| 缺少 ServerID | 保持 400 |
| 缺少或错误 Token | 保持 401 |
| ServerID 不存在或已删除 | 保持 404；不自动创建、别名跳转或重新认领 |
| 合法 ID/Token，InstallID 尚未绑定 | 继续工作，身份管理显示待登记，不自动合并 |
| 合法 ID/Token，InstallID 已绑定 | 继续原流程，无协议变化 |

### 6.1 明确的能力限制

InstallID 是安装生命周期锚点，不是心跳的独立证明。保持现有协议意味着后端无法从心跳重新验证宿主机身份文件。

如果整套 InstallID、ServerID、AgentToken 被克隆到另一台机器，当前协议无法可靠区分两者。本方案只能通过克隆部署规范、显式重新登记及凭据保护降低风险；地址变化不能被当成确凿的克隆证明。硬件证明、Agent 密钥挑战或协议扩展必须另立任务，不在本方案中偷偷加入。

## 7. 合并规则

### 7.1 默认禁止自动合并

首次登记与幂等登记不是合并。正常启动、GET、心跳、同步、版本升级、地址变化均不得删除另一条 Server。

| 场景 | 处理 |
| --- | --- |
| AgentVersion 不同 | 不据此合并 |
| AgentVersion 相同 | 不据此合并 |
| IP/Host 相同，身份不同或无法确认 | 保留，提示需要核验 |
| Token 相同，但没有可靠安装绑定依据 | 保留；共享凭据不是身份的充分证明 |
| 不同 InstallID | 视为不同安装实例，禁止身份合并 |
| 相同 InstallID 对应多条 Server | 视为数据冲突，不自动挑选胜者 |
| 已唯一绑定的同一 InstallID 重复登记 | 返回原 ServerID；不动业务引用 |

因此，不能把 store 中旧版本常量简单替换成新版本；也不能只把 map 排序当成身份修复。

### 7.2 管理员批准的历史去重

历史去重只用于已经核实同一安装实例的重复记录。管理员需要指定保留 ServerID 和待移除 ID，依据安装身份文件、备份、登记记录及实际运行配置确认；无法证明时拒绝执行。

1. 只读预演，列出完整差异、所有引用、Agent 配置和待执行系统操作；不凭在线状态或版本自行选择保留 ID。
2. 确认源身份没有独立运行实例；如需调整本地 agent.env，先取得运维授权并协调停止/切换，不能产生双 Agent 控制同一套入站。
3. 检查目标原有 Node/Relay 端口冲突、LocalServerID、待执行 BBR 操作等。冲突必须阻止提交；不能静默改端口、协议、Reality 参数或丢弃动作。
4. 在工作副本中同时处理 `Node.ServerID`、`RelayRoute.RelayServerID` 和必要的 `LocalServerID`；Node.ID、RelayRoute.ID 不变，所以 Client 和 LandingNode 引用不得被重建。
5. 保留目标的明确身份及凭据，不从较新版本记录任意复制 Token；源凭据的退役要与 Agent 配置切换协调完成。
6. 校验整个引用图后才删除源记录，所有结构更新一起持久化；保存失败不发布新内存状态。
7. 操作记录包含批准者、源/目标 ID、备份标识和变更摘要，不包含 Token、客户信息或私钥。

只读预演得到的数据摘要必须作为提交前置条件。确认后若数据已经变化，返回冲突并重新预演，不沿用过期方案强行合并。

## 8. 删除规则

### 8.1 删除前置检查

删除必须是显式管理员写操作，在同一锁内重新检查条件：

| 检查项 | 处理 |
| --- | --- |
| 当前本机 Server 或单机模式最后一条 Server | 默认禁止删除；先明确重新登记本机，不能自动选替代者 |
| 任意 Node.ServerID 引用，包括禁用 Node | 拒绝删除，列出需要先处理的引用 |
| 任意 RelayRoute.RelayServerID 引用，包括禁用 Relay | 拒绝删除，不能靠 enabled=false 忽略它 |
| 通过落地 Node 产生的间接依赖 | 必须随完整 Node/Relay 引用检查覆盖 |
| 待执行 BBR 等 Server 专属操作 | 要求先确认完成或明确取消，不能静默移给另一台 |
| Agent 仍在运行 | 提醒先停止该身份的 Agent 并确认；这不授权安装器远程停服务 |
| 数据本身已有身份冲突或与当前预演不符 | 停止删除，输出原因 |

校验失败建议返回 409 和脱敏的引用 ID 列表；这是未来管理 API 行为调整，实施前需检查前端错误提示兼容。Agent 认证失败状态保持上一节的兼容规则。

没有引用且经确认退役时，删除记录会使原 ID/Token 失效。不能自动删除客户、入站、中转线路、出口或宿主机身份文件。

重复删除不得误删其他记录；返回未找到即可，不改变为任意地址匹配删除。Agent 随后请求原 ID 返回 404，不能自动恢复记录。

### 8.2 创建、编辑也必须保护引用

未来护栏必须覆盖引用创建/编辑与 Server 删除的共同写边界：在同一锁内校验目标 Server 仍存在。否则“先检查后被删，再写入 RelayRoute”仍可能产生孤儿。

该护栏只管理引用完整性，不修改线路模式、客户固定出口、Xray 生成或 Agent apply 判断。不能只在删除按钮做前端判断，也不能只在 doctor 事后提示。

## 9. 数据迁移策略

### 9.1 分阶段，逐次授权

| 阶段 | 内容 | 数据/协议边界 |
| --- | --- | --- |
| D0：本次设计 | 本文档与只读模型核对 | 无任何数据库或运行时改动 |
| R1：非破坏性护栏 | 移除版本参与保留逻辑，禁止自动跨身份合并，补齐删除与引用写入检查，消除读取触发的删除 | 需要单独批准源码实现；不新增身份数据，不改 Agent 协议 |
| R2：增量安装身份 | 增加 InstallID/IdentitySchema/LocalServerID、管理登记及安装器持久化 | 需要单独批准数据格式、管理 API 和部署兼容性变更；Agent 协议保持不变 |
| R3：历史清理 | 根据人工批准的映射修复孤儿或去重 | 高风险独立运维任务，不随升级自动执行 |

R1 必须先稳定，不能一开始就让所有旧 Server 获得“猜出来”的 InstallID。实施使用隔离分支/工作树及回归测试，当前未提交的 BBR 与版本一致性成果必须保留。

### 9.2 只读预检

预检读取原始 JSON，不能调用带有自动合并副作用的旧 `Store.Open()`；不启动生产服务。

- 校验 JSON、文件摘要、Server map 键与 ID、重复非空 InstallID、LocalServerID、Node/Relay/Client 引用。
- 记录旧数据数量、主键与凭据摘要，真实凭据不打印、不上传。
- 区分“地址相同的疑似重复”与“已证明同一安装实例”，不能把诊断结果自动当迁移授权。
- 合法历史缺少 InstallID 不算错误，可以长期保留为待绑定；不能人为填充默认 UUID 使检查变绿。
- 发现孤儿或歧义时保留原始证据，停止身份写迁移，先等待批准处理；不自动删除、禁用或重新绑定路线。

### 9.3 首次绑定与增量格式

1. 对本机旧 Agent，从宿主机 `agent.env` 读取明确的 ServerID/Token，核对对应 Server，并由管理员批准首次绑定。不能用 IP、在线状态或名称评分替代。
2. 远程 Server 由对应宿主机安装器生成 InstallID，经管理员核验原凭据后逐台绑定；面板不能给未知远程实例凭空分配身份。
3. 缺少 agent.env、原 Server 已删除或凭据不匹配时保持未绑定并报告，不能自动创建替代记录。
4. 在合法数据中加入可缺省字段、设置 `identity_schema=1`。该标志表示格式能力，不表示所有历史 Agent 已完成绑定。
5. 保持原 ServerID、AgentToken、客户 UUID/订阅 Token、Node/Relay ID、端口、协议、Reality 参数、出口和网络策略原样。
6. 迁移可重复执行：匹配映射不重复生成 ID；相同输入得到空变更；冲突不覆盖。

未知 JSON 字段也必须在备份与迁移中保留，不能因为用旧结构反序列化再序列化而丢失。产品版本字段与身份 schema 分别处理，不用 `PanelData.Version` 判断是否必须改身份。

### 9.4 既有孤儿引用

如果 RelayServerID 指向不存在的记录：

- 优先从可信备份与运维记录查找原 Server 和运行配置，形成逐条处理方案。
- 能证明误删且原身份仍合法时，评估恢复原 ID/凭据，不按地址创建冒牌替代 Server。
- 能证明属于已批准的同一实例迁移时，评估更新引用到明确目标。
- 不能证明时保留并报告，不根据 AgentVersion、IP 或第一条 Server 猜目标。
- 上述恢复或引用修改都是另外的业务数据操作，必须单独授权、备份和验收。本次与默认升级都不执行。

### 9.5 一致性与提交

单文件 JSON 没有关系数据库外键，必须在 store 写边界承担约束：

1. 在线管理操作在同一 `Mu` 写锁内核验当前数据及预演摘要，在深复制的工作副本上修改、校验。
2. 先写同文件系统临时文件，检查写入、同步、替换结果；完整成功后再发布正式内存状态和成功响应。
3. 离线批量迁移必须经批准停止所有会写 JSON 的进程，保存一致的备份和摘要；不能只停 API 却让另一个安装/恢复脚本继续写。
4. JSON 与 agent.env/身份文件之间不是跨文件事务。迁移清单应记录各文件前后摘要及完成阶段；失败保留原凭据，通过同一授权操作恢复，不换 ID 逃避失败。
5. 写权限、磁盘空间、损坏输入、并发变化、取消操作等失败均必须有明确错误和可恢复结果，不无限重试、不吞错。

备份应覆盖 JSON、身份文件、agent.env、本机选择、部署配置和匹配的可执行版本；root 私有保存、验证可读及 SHA256。备份不得进入 Release 或公开日志。

## 10. 回滚方案

### 10.1 本次文档回滚

本次只新增本文档，无运行时或数据影响。若不采用，确认后仅移除本次新增文档即可；不能 reset、checkout、stash 或覆盖已有 BBR/版本修改。本次未执行移除。

### 10.2 未来实施回滚

不能承诺直接装回当前旧版本就安全：旧代码会忽略新增字段，并可能再次自动合并；旧结构保存还可能丢失新增字段。

1. R2 开始前必须准备并验证安全回退构建：保留 R1 的非破坏性护栏，并能保存当前身份增量字段。当前基线不是已验证的安全回退构建。
2. 只回退新登记能力、继续运行兼容构建时，停止新身份绑定写操作，保持已有 ServerID/Token/InstallID 及引用图，不删除增量字段。
3. 需要恢复迁移前格式时，先进入经批准的维护窗口，停止所有写入者，保存故障现场和迁移后的完整备份。
4. 如果迁移后没有业务写入，核验后成组恢复迁移前 JSON、agent.env、身份文件、本机绑定及匹配运行版本，启动前做只读引用校验。
5. 如果迁移后已有客户、入站、线路或凭据变化，不能直接恢复旧 JSON 丢弃这些变化。必须制作逆向迁移/差异恢复计划，无法无损恢复时停止并人工确认。
6. 已执行的历史合并/删除需要用操作映射、备份和匹配的 Agent 配置恢复，不能仅降级二进制；若有新引用或端口冲突，不能自动反向拆分。
7. 恢复后逐项验证 ID/Token 认证、Nodes/Relay/Client 归属、安装幂等和原有链路。版本、时间戳或 IP 不能替代验收。

旧安装脚本可能覆盖配置或触发旧 store 行为，因此不能把“运行旧 install.sh”当成通用回滚命令。具体命令必须在未来实施版本、备份位置和维护授权确定后提供，本文不执行任何恢复操作。

## 11. 未来验收标准

以下测试尚未执行于修复版本；只作为后续实现门槛，不等同于上一轮风险复现结果。

| 测试 | 必须满足的结果 |
| --- | --- |
| Case A 不同版本、交换顺序 | 身份与引用不变化；版本更新不能决定删除哪台 Server |
| Case B 相同版本/地址、不同身份 | 两条记录及各自归属保留；原 ID/Token 都能认证 |
| Case C 自动合并与普通删除 | 未授权不删除；有任意中转引用时拒绝；不产生孤儿 |
| 重复注册、并发注册、安装中断恢复 | 同一已批准实例只登记一次，不更换 ID/Token |
| ID/Token 错配、未知/已删除 ID | 精确拒绝，不自动认领其他记录 |
| 缺失/损坏身份文件、克隆/恢复 | 明确报错或经授权恢复，不按地址猜测身份 |
| 老 Agent/未绑定 Server | 原心跳与同步字段和状态码兼容，业务不被自动迁移 |
| fast/Docker 切换、升级、重复安装 | 身份、凭据和引用保持稳定 |
| 无引用 Server 删除、本机/最后一台删除 | 前者经授权可退役；后者被明确保护 |
| 禁用 Node/Relay、引用创建与删除并发 | 禁用不忽略引用；写操作不能竞态造孤儿 |
| 普通 Server PUT 缺少身份字段 | 不清空或替换身份与凭据 |
| 磁盘满、只读目录、提交失败 | 不返回成功，不发布半完成的内存状态 |
| 迁移 dry-run、重复迁移、逆向迁移 | 预检只读；重复无额外变更；失败证据可保留 |
| 已有孤儿与历史歧义 | 报告并停止迁移，不自动修复 |
| 原有 Xray/Reality/客户/订阅/Agent apply | 不改生成规则或协议，原有回归与真机测试通过 |

后续验证应在隔离目录/测试机执行 Go 测试、Linux 构建、相关安装脚本语法与幂等性测试，再做经授权的升级/回滚真机验证。没有执行的测试必须标为未执行。

## 12. 实施边界与待确认事项

- 本方案选择“安装管理侧登记 + 原 ID/Token 通信”，不选择当前阶段的 Agent 新注册/心跳协议。
- 身份字段落在 model，约束与持久化落在 store，授权登记落在管理 API，文件生命周期落在安装器；不能把身份推断塞入 Xray 或 store 的通用 normalize。
- 未来确需涉及的安装器、默认身份解析、Server 写入口和引用检查，应在独立实施计划中逐文件列出；本文不授权直接修改它们。
- `legacy_unbound` 是数据兼容状态，不是第二套 Agent 协议；正式认证只有原有 ServerID/Token 一条路径。历史无身份字段的支持不能在未通知和未完成逐台迁移前删除。
- 管理员审批能力、登记接口权限、回退构建与克隆运维规范需要确认后实现；InstallID 不解决凭据被盗或完整克隆的持续认证问题。
- 本次未运行构建、修改版单元测试、数据迁移、注册接口或真机测试，因为交付是文档且没有实现代码。
- 本次没有新增依赖、生产临时代码或备用执行路径，也没有清理、删除或格式化任何现有代码。

## 13. 源码核对位置

以下为本次设计实际阅读的相对仓库路径及当前行号，不指向线上数据：

| 位置 | 核对内容 |
| --- | --- |
| `backend/internal/model/model.go:17` | Server 当前字段 |
| `backend/internal/model/model.go:62` | Node 与 Server 引用 |
| `backend/internal/model/model.go:108` | Client 的 Node/Relay 引用 |
| `backend/internal/model/model.go:128` | RelayRoute 两侧引用 |
| `backend/internal/model/model.go:186` | 既有心跳与同步模型 |
| `backend/internal/model/model.go:220` | PanelData 当前格式 |
| `backend/internal/store/store.go:25` | 加载与初始化入口 |
| `backend/internal/store/store.go:175` | 保存和 normalize 边界 |
| `backend/internal/store/store.go:218` | 候选选取、节点迁移与 Server 删除 |
| `backend/internal/store/store.go:308` | 硬编码版本参与保留规则 |
| `backend/internal/api/servers.go:13` | 读取、创建与普通编辑 |
| `backend/internal/api/servers.go:83` | 普通删除的现有引用检查 |
| `backend/internal/api/agent.go:13` | 心跳更新 |
| `backend/internal/api/agent.go:54` | 同步筛选和既有配置生成调用 |
| `backend/internal/api/agent.go:130` | ServerID/Token 认证 |
| `backend/internal/api/nodes.go:135` | 默认服务器当前按状态/同步时间选择 |
| `backend/internal/api/clients.go:229` | 客户创建中的默认中转 Server 解析 |
| `backend/internal/api/relays.go:140` | 中转 Server 校验 |
| `backend/internal/api/bbr_optimization.go:77` | Server 专属待执行操作 |
| `deploy/install.sh:603` | 本机 Agent 当前选择与配置流程 |
| `deploy/agent-install.sh:104` | 当前 agent.env 写入 |
| `agent/cmd/agent/main.go:88` | Agent 原配置与启动流程 |
| `agent/cmd/agent/main.go:151` | 既有同步与心跳发送 |

已有 `docs/ARCHITECTURE.md`、`docs/API.md` 的架构说明较早，当前行为以实际源码为准。本次只补充本文，不顺手重写历史文档。
