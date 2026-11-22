CREATE TABLE IF NOT EXISTS jenkins_connections (
    id VARCHAR(80) NOT NULL COMMENT '连接标识',
    name VARCHAR(120) NOT NULL DEFAULT '' COMMENT '连接名称',
    url VARCHAR(500) NOT NULL DEFAULT '' COMMENT 'Jenkins 服务地址',
    username VARCHAR(160) NOT NULL DEFAULT '' COMMENT '服务账号',
    token VARCHAR(300) NOT NULL DEFAULT '' COMMENT 'Jenkins API 令牌',
    project_ids VARCHAR(4000) NOT NULL DEFAULT '[]' COMMENT '授权项目标识的 JSON 数组',
    all_projects BOOLEAN NOT NULL DEFAULT FALSE COMMENT '允许所有项目使用',
    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Jenkins 服务连接';

CREATE TABLE IF NOT EXISTS jenkins_bindings (
    project_id VARCHAR(100) NOT NULL COMMENT '所属项目',
    id VARCHAR(80) NOT NULL COMMENT '项目内绑定标识',
    connection_id VARCHAR(80) NOT NULL DEFAULT '' COMMENT '连接标识',
    service VARCHAR(120) NOT NULL DEFAULT '' COMMENT '服务名称',
    environment VARCHAR(80) NOT NULL DEFAULT '' COMMENT '部署环境',
    job VARCHAR(500) NOT NULL DEFAULT '' COMMENT 'Jenkins 完整任务名',
    parameter VARCHAR(80) NOT NULL DEFAULT '' COMMENT '可选任务参数名',
    value VARCHAR(240) NOT NULL DEFAULT '' COMMENT '可选任务参数值',
    parameters VARCHAR(4000) NOT NULL DEFAULT '{}' COMMENT '任务参数 JSON 对象',
    parameterized BOOLEAN NOT NULL DEFAULT FALSE COMMENT '参数化任务标记',
    PRIMARY KEY (project_id, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='项目 Jenkins 发布任务绑定';

CREATE TABLE IF NOT EXISTS jenkins_receipts (
    run_id VARCHAR(100) NOT NULL COMMENT 'SOP 运行标识',
    node_id VARCHAR(100) NOT NULL COMMENT '发布节点标识',
    attempt BIGINT NOT NULL DEFAULT 0 COMMENT '旧版节点尝试号或固定发布记录',
    url VARCHAR(1500) NOT NULL DEFAULT '' COMMENT 'Jenkins 接单链接',
    state VARCHAR(16) NOT NULL DEFAULT 'pending' COMMENT '发布请求状态',
    queued BOOLEAN NOT NULL DEFAULT FALSE COMMENT '接单链接指向队列',
    PRIMARY KEY (run_id, node_id, attempt)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Jenkins 发布触发回执';
