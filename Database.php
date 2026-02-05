<?php

class Database
{
    private $pdo;
    private $dbFile;

    public function __construct($dbFile = null)
    {
        // 默认路径修改为 /data/ 子目录
        $this->dbFile = $dbFile ?: __DIR__ . '/data/data.sqlite';

        // 环境安全检查
        $this->secureEnvironment();

        $this->connect();
        $this->initSchema();
    }

    private function secureEnvironment()
    {
        $dir = dirname($this->dbFile);
        $oldFile = __DIR__ . '/data.sqlite';

        // 1. 自动创建目录
        if (!is_dir($dir)) {
            if (!@mkdir($dir, 0755, true)) {
                $this->throwPermissionError($dir);
            }
        }

        // 2. 自动迁移旧数据
        if (file_exists($oldFile) && !file_exists($this->dbFile)) {
            if (!@rename($oldFile, $this->dbFile)) {
                if (@copy($oldFile, $this->dbFile)) {
                    @unlink($oldFile);
                } else {
                    throw new Exception("安全迁移失败：无法移动旧数据库。请检查目录权限。");
                }
            }
        }

        // 3. 部署 .htaccess
        $htaccess = $dir . '/.htaccess';
        if (!file_exists($htaccess)) {
            @file_put_contents($htaccess, "Order Deny,Allow\nDeny from all");
        }

        // 4. 部署 index.html
        $indexHtml = $dir . '/index.html';
        if (!file_exists($indexHtml)) {
            @file_put_contents($indexHtml, '');
        }
    }

    private function connect()
    {
        try {
            $this->pdo = new PDO('sqlite:' . $this->dbFile);
            $this->pdo->setAttribute(PDO::ATTR_ERRMODE, PDO::ERRMODE_EXCEPTION);
            $this->pdo->setAttribute(PDO::ATTR_DEFAULT_FETCH_MODE, PDO::FETCH_ASSOC);
            // 开启 WAL 模式，提高并发读写性能
            $this->pdo->exec('PRAGMA journal_mode = WAL;');
        } catch (PDOException $e) {
            if (strpos($e->getMessage(), 'unable to open database file') !== false) {
                $this->throwPermissionError(dirname($this->dbFile));
            }
            throw new Exception("Database Error: " . $e->getMessage());
        }
    }

    private function throwPermissionError($dir)
    {
        $user = get_current_user();
        throw new Exception("权限不足：Web用户 ({$user}) 无法读写 {$dir}。<br>请修复权限：<code>chown -R {$user}:{$user} " . __DIR__ . "</code>");
    }

    private function initSchema()
    {
        $this->pdo->exec("CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT)");

        $this->pdo->exec("CREATE TABLE IF NOT EXISTS accounts (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            access_key_id TEXT,
            access_key_secret TEXT,
            region_id TEXT,
            instance_id TEXT,
            max_traffic REAL,
            schedule_enabled INTEGER DEFAULT 0,
            start_time TEXT,
            stop_time TEXT,
            traffic_used REAL DEFAULT 0,
            instance_status TEXT DEFAULT 'Unknown',
            updated_at INTEGER DEFAULT 0,
            last_keep_alive_at INTEGER DEFAULT 0
        )");

        $this->pdo->exec("CREATE TABLE IF NOT EXISTS logs (id INTEGER PRIMARY KEY AUTOINCREMENT, type TEXT, message TEXT, created_at INTEGER)");

        $this->pdo->exec("CREATE TABLE IF NOT EXISTS login_attempts (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            ip TEXT,
            attempt_time INTEGER
        )");

        // 1. 小时级表 (24小时折线图)
        $this->pdo->exec("CREATE TABLE IF NOT EXISTS traffic_hourly (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            account_id INTEGER,
            traffic REAL,
            recorded_at INTEGER
        )");
        $this->pdo->exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_traffic_hourly_unique ON traffic_hourly (account_id, recorded_at)");

        // 2. 天级表 (30天柱状图)
        $this->pdo->exec("CREATE TABLE IF NOT EXISTS traffic_daily (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            account_id INTEGER,
            traffic REAL,
            recorded_at INTEGER
        )");
        $this->pdo->exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_traffic_daily_unique ON traffic_daily (account_id, recorded_at)");

        $this->ensureColumn('accounts', 'traffic_used', 'REAL DEFAULT 0');
        $this->ensureColumn('accounts', 'instance_status', "TEXT DEFAULT 'Unknown'");
        $this->ensureColumn('accounts', 'updated_at', 'INTEGER DEFAULT 0');
        $this->ensureColumn('accounts', 'last_keep_alive_at', 'INTEGER DEFAULT 0');

        $this->migrateStatsToAccountId();
    }

    private function ensureColumn($table, $column, $definition)
    {
        try {
            $this->pdo->query("SELECT $column FROM $table LIMIT 1");
        } catch (Exception $e) {
            $this->pdo->exec("ALTER TABLE $table ADD COLUMN $column $definition");
        }
    }

    private function migrateStatsToAccountId()
    {
        // check if traffic_hourly has access_key_id column
        $needsMigration = false;
        try {
            $this->pdo->query("SELECT access_key_id FROM traffic_hourly LIMIT 1");
            $needsMigration = true;
        } catch (Exception $e) {
            // column does not exist, no migration needed (or already migrated)
        }

        if ($needsMigration) {
            try {
                $this->pdo->beginTransaction();

                // 1. Migrate hourly stats
                $this->pdo->exec("CREATE TABLE traffic_hourly_new (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    account_id INTEGER,
                    traffic REAL,
                    recorded_at INTEGER
                )");

                // Try to link stats to the first matching account ID for that AK
                $this->pdo->exec("INSERT INTO traffic_hourly_new (account_id, traffic, recorded_at)
                    SELECT a.id, t.traffic, t.recorded_at 
                    FROM traffic_hourly t
                    JOIN accounts a ON t.access_key_id = a.access_key_id
                    GROUP BY a.id, t.recorded_at"); // Group by to avoid duplicates if multiple accounts share AK (though previous schema didn't allow duplicates, this is safety)

                $this->pdo->exec("DROP TABLE traffic_hourly");
                $this->pdo->exec("ALTER TABLE traffic_hourly_new RENAME TO traffic_hourly");
                $this->pdo->exec("CREATE UNIQUE INDEX idx_traffic_hourly_unique ON traffic_hourly (account_id, recorded_at)");

                // 2. Migrate daily stats
                $this->pdo->exec("CREATE TABLE traffic_daily_new (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    account_id INTEGER,
                    traffic REAL,
                    recorded_at INTEGER
                )");

                $this->pdo->exec("INSERT INTO traffic_daily_new (account_id, traffic, recorded_at)
                    SELECT a.id, t.traffic, t.recorded_at 
                    FROM traffic_daily t
                    JOIN accounts a ON t.access_key_id = a.access_key_id
                    GROUP BY a.id, t.recorded_at");

                $this->pdo->exec("DROP TABLE traffic_daily");
                $this->pdo->exec("ALTER TABLE traffic_daily_new RENAME TO traffic_daily");
                $this->pdo->exec("CREATE UNIQUE INDEX idx_traffic_daily_unique ON traffic_daily (account_id, recorded_at)");

                $this->pdo->commit();
            } catch (Exception $e) {
                if ($this->pdo->inTransaction())
                    $this->pdo->rollBack();
                // Log error but don't stop execution, schema might be in mixed state but next run/update might fix or user can reset
                $this->addLog('error', "Database Migration Failed: " . $e->getMessage());
            }
        }
    }

    public function getPdo()
    {
        return $this->pdo;
    }

    public function addLog($type, $message)
    {
        $stmt = $this->pdo->prepare("INSERT INTO logs (type, message, created_at) VALUES (?, ?, ?)");
        $stmt->execute([$type, $message, time()]);
    }

    public function getLogs($limit = 100)
    {
        $stmt = $this->pdo->prepare("SELECT * FROM logs ORDER BY id DESC LIMIT ?");
        $stmt->execute([$limit]);
        return $stmt->fetchAll(PDO::FETCH_ASSOC);
    }

    public function getLogsByTypes(array $types, $limit = 20)
    {
        $placeholders = implode(',', array_fill(0, count($types), '?'));
        $sql = "SELECT * FROM logs WHERE type IN ($placeholders) ORDER BY id DESC LIMIT ?";

        $params = $types;
        $params[] = $limit;

        $stmt = $this->pdo->prepare($sql);
        $stmt->execute($params);
        return $stmt->fetchAll(PDO::FETCH_ASSOC);
    }

    public function clearLogsByTypes(array $types)
    {
        $placeholders = implode(',', array_fill(0, count($types), '?'));
        $stmt = $this->pdo->prepare("DELETE FROM logs WHERE type IN ($placeholders)");
        return $stmt->execute($types);
    }

    /**
     * 优化后的日志清理逻辑
     * @param int $defaultDays 默认保留天数（用于重要日志）
     * @param int $heartbeatDays 心跳日志保留天数（建议设置较短，如3天）
     */
    public function pruneLogs($defaultDays = 30, $heartbeatDays = 3)
    {
        $now = time();

        // 1. 清理过期心跳日志 (Heartbeat) - 激进清理
        $stmt = $this->pdo->prepare("DELETE FROM logs WHERE type = 'heartbeat' AND created_at < ?");
        $stmt->execute([$now - ($heartbeatDays * 86400)]);

        // 2. 清理其他过期日志 (Info, Warning, Error) - 保守清理
        $stmt = $this->pdo->prepare("DELETE FROM logs WHERE type != 'heartbeat' AND created_at < ?");
        $stmt->execute([$now - ($defaultDays * 86400)]);
    }

    /**
     * 重置 Logs 表的自增 ID 并重新排序
     * 这是一个较重的操作，但能保证 ID 连续
     */
    public function reorderLogsIds()
    {
        try {
            $this->pdo->beginTransaction();

            // 1. 检查是否有数据
            $count = $this->pdo->query("SELECT COUNT(*) FROM logs")->fetchColumn();

            if ($count == 0) {
                // 如果没数据，直接重置序号为 0
                $this->pdo->exec("DELETE FROM sqlite_sequence WHERE name='logs'");
                $this->pdo->exec("DELETE FROM logs"); // 确保空
            } else {
                // 2. 使用临时表重排数据
                // 创建临时表保存现有数据，按时间正序排列
                $this->pdo->exec("CREATE TEMPORARY TABLE logs_temp AS SELECT type, message, created_at FROM logs ORDER BY created_at ASC");

                // 清空原表
                $this->pdo->exec("DELETE FROM logs");

                // 重置自增序列
                $this->pdo->exec("DELETE FROM sqlite_sequence WHERE name='logs'");

                // 将数据插回原表，ID 会自动从 1 开始重新生成
                $this->pdo->exec("INSERT INTO logs (type, message, created_at) SELECT type, message, created_at FROM logs_temp");

                // 删除临时表
                $this->pdo->exec("DROP TABLE logs_temp");
            }

            $this->pdo->commit();
            return true;
        } catch (Exception $e) {
            if ($this->pdo->inTransaction()) {
                $this->pdo->rollBack();
            }
            // 记录错误到错误日志文件（如果有），或者忽略，因为这不是关键业务
            return false;
        }
    }

    /**
     * 整理数据库碎片 (VACUUM)
     * 释放已删除数据占用的磁盘空间
     */
    public function vacuum()
    {
        $this->pdo->exec("VACUUM");
    }

    // --- 登录频率限制相关方法 ---

    public function recordLoginAttempt($ip)
    {
        $stmt = $this->pdo->prepare("INSERT INTO login_attempts (ip, attempt_time) VALUES (?, ?)");
        $stmt->execute([$ip, time()]);
    }

    public function getRecentFailedAttempts($ip, $windowSeconds = 900)
    {
        $stmt = $this->pdo->prepare("SELECT COUNT(*) FROM login_attempts WHERE ip = ? AND attempt_time > ?");
        $stmt->execute([$ip, time() - $windowSeconds]);
        return (int) $stmt->fetchColumn();
    }

    public function clearLoginAttempts($ip)
    {
        $stmt = $this->pdo->prepare("DELETE FROM login_attempts WHERE ip = ?");
        $stmt->execute([$ip]);
    }

    // --- 流量记录逻辑 ---

    public function addHourlyStat($accountId, $traffic)
    {
        $hourTimestamp = floor(time() / 3600) * 3600;
        $stmt = $this->pdo->prepare("INSERT OR REPLACE INTO traffic_hourly (account_id, traffic, recorded_at) VALUES (?, ?, ?)");
        $stmt->execute([$accountId, $traffic, $hourTimestamp]);
    }

    public function addDailyStat($accountId, $traffic)
    {
        $dayTimestamp = strtotime(date('Y-m-d 00:00:00'));
        $stmt = $this->pdo->prepare("INSERT OR REPLACE INTO traffic_daily (account_id, traffic, recorded_at) VALUES (?, ?, ?)");
        $stmt->execute([$accountId, $traffic, $dayTimestamp]);
    }

    public function getHourlyStats($accountId)
    {
        $stmt = $this->pdo->prepare("SELECT traffic, recorded_at FROM traffic_hourly WHERE account_id = ? ORDER BY recorded_at DESC LIMIT 25");
        $stmt->execute([$accountId]);
        $data = $stmt->fetchAll(PDO::FETCH_ASSOC);
        return array_reverse($data);
    }

    public function getDailyStats($accountId)
    {
        $stmt = $this->pdo->prepare("SELECT traffic, recorded_at FROM traffic_daily WHERE account_id = ? ORDER BY recorded_at DESC LIMIT 31");
        $stmt->execute([$accountId]);
        $data = $stmt->fetchAll(PDO::FETCH_ASSOC);
        return array_reverse($data);
    }

    public function pruneStats()
    {
        $hourLimit = time() - (48 * 3600);
        $this->pdo->exec("DELETE FROM traffic_hourly WHERE recorded_at < $hourLimit");

        $dayLimit = time() - (60 * 86400);
        $this->pdo->exec("DELETE FROM traffic_daily WHERE recorded_at < $dayLimit");
    }
}