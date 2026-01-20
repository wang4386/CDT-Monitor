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

        // --- 新增：独立的小时级和天级流量表 ---
        
        // 1. 小时级表 (24小时折线图)
        $this->pdo->exec("CREATE TABLE IF NOT EXISTS traffic_hourly (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            access_key_id TEXT,
            traffic REAL,
            recorded_at INTEGER
        )");
        // 唯一索引：确保每个 AK 在每个小时（时间戳归一化后）只有一条记录
        $this->pdo->exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_traffic_hourly_unique ON traffic_hourly (access_key_id, recorded_at)");

        // 2. 天级表 (30天柱状图)
        $this->pdo->exec("CREATE TABLE IF NOT EXISTS traffic_daily (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            access_key_id TEXT,
            traffic REAL,
            recorded_at INTEGER
        )");
        // 唯一索引：确保每个 AK 在每天（时间戳归一化后）只有一条记录
        $this->pdo->exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_traffic_daily_unique ON traffic_daily (access_key_id, recorded_at)");

        $this->ensureColumn('accounts', 'traffic_used', 'REAL DEFAULT 0');
        $this->ensureColumn('accounts', 'instance_status', "TEXT DEFAULT 'Unknown'");
        $this->ensureColumn('accounts', 'updated_at', 'INTEGER DEFAULT 0');
        $this->ensureColumn('accounts', 'last_keep_alive_at', 'INTEGER DEFAULT 0');
    }

    private function ensureColumn($table, $column, $definition)
    {
        try {
            $this->pdo->query("SELECT $column FROM $table LIMIT 1");
        } catch (Exception $e) {
            $this->pdo->exec("ALTER TABLE $table ADD COLUMN $column $definition");
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

    public function pruneLogs($days = 30)
    {
        $stmt = $this->pdo->prepare("DELETE FROM logs WHERE created_at < ?");
        $stmt->execute([time() - ($days * 86400)]);
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
        return (int)$stmt->fetchColumn();
    }

    public function clearLoginAttempts($ip)
    {
        $stmt = $this->pdo->prepare("DELETE FROM login_attempts WHERE ip = ?");
        $stmt->execute([$ip]);
    }

    // --- 新的流量记录逻辑 ---

    /**
     * 记录小时级数据
     * 利用 UNIQUE INDEX 和 INSERT OR IGNORE 实现“每小时只记一条”
     */
    public function addHourlyStat($accessKeyId, $traffic)
    {
        // 归一化到当前小时的整点 (例如 10:23 -> 10:00)
        $hourTimestamp = floor(time() / 3600) * 3600;
        
        $stmt = $this->pdo->prepare("INSERT OR IGNORE INTO traffic_hourly (access_key_id, traffic, recorded_at) VALUES (?, ?, ?)");
        $stmt->execute([$accessKeyId, $traffic, $hourTimestamp]);
    }

    /**
     * 记录天级数据
     * 利用 UNIQUE INDEX 和 INSERT OR IGNORE 实现“每天只记一条”
     */
    public function addDailyStat($accessKeyId, $traffic)
    {
        // 归一化到当天的 00:00
        $dayTimestamp = strtotime(date('Y-m-d 00:00:00'));
        
        $stmt = $this->pdo->prepare("INSERT OR IGNORE INTO traffic_daily (access_key_id, traffic, recorded_at) VALUES (?, ?, ?)");
        $stmt->execute([$accessKeyId, $traffic, $dayTimestamp]);
    }

    /**
     * 获取最近 24 小时的数据
     */
    public function getHourlyStats($accessKeyId)
    {
        // 获取最近 25 条，保证覆盖24小时
        $stmt = $this->pdo->prepare("SELECT traffic, recorded_at FROM traffic_hourly WHERE access_key_id = ? ORDER BY recorded_at DESC LIMIT 25");
        $stmt->execute([$accessKeyId]);
        $data = $stmt->fetchAll(PDO::FETCH_ASSOC);
        // 按时间正序排列返回
        return array_reverse($data);
<<<<<<< Updated upstream
<<<<<<< Updated upstream
=======
=======
    }

    /**
     * 获取最近 30 天的数据
     */
    public function getDailyStats($accessKeyId)
    {
        $stmt = $this->pdo->prepare("SELECT traffic, recorded_at FROM traffic_daily WHERE access_key_id = ? ORDER BY recorded_at DESC LIMIT 31");
        $stmt->execute([$accessKeyId]);
        $data = $stmt->fetchAll(PDO::FETCH_ASSOC);
        return array_reverse($data);
    }

    /**
     * 清理过期统计数据
     */
    public function pruneStats()
    {
        // 1. 清理小时表：保留最近 24+2 小时以外的数据
        // 既然我们只取 Limit 24，其实可以删掉 48 小时前的
        $hourLimit = time() - (48 * 3600);
        $this->pdo->exec("DELETE FROM traffic_hourly WHERE recorded_at < $hourLimit");

        // 2. 清理天表：保留最近 60 天以外的 (留点余量)
        $dayLimit = time() - (60 * 86400);
        $this->pdo->exec("DELETE FROM traffic_daily WHERE recorded_at < $dayLimit");
>>>>>>> Stashed changes
    }

    /**
     * 获取最近 30 天的数据
     */
    public function getDailyStats($accessKeyId)
    {
        $stmt = $this->pdo->prepare("SELECT traffic, recorded_at FROM traffic_daily WHERE access_key_id = ? ORDER BY recorded_at DESC LIMIT 31");
        $stmt->execute([$accessKeyId]);
        $data = $stmt->fetchAll(PDO::FETCH_ASSOC);
        return array_reverse($data);
    }

    /**
     * 清理过期统计数据
     */
    public function pruneStats()
    {
        // 1. 清理小时表：保留最近 24+2 小时以外的数据
        // 既然我们只取 Limit 24，其实可以删掉 48 小时前的
        $hourLimit = time() - (48 * 3600);
        $this->pdo->exec("DELETE FROM traffic_hourly WHERE recorded_at < $hourLimit");

        // 2. 清理天表：保留最近 60 天以外的 (留点余量)
        $dayLimit = time() - (60 * 86400);
        $this->pdo->exec("DELETE FROM traffic_daily WHERE recorded_at < $dayLimit");
>>>>>>> Stashed changes
    }

    /**
     * 获取最近 30 天的数据
     */
    public function getDailyStats($accessKeyId)
    {
        $stmt = $this->pdo->prepare("SELECT traffic, recorded_at FROM traffic_daily WHERE access_key_id = ? ORDER BY recorded_at DESC LIMIT 31");
        $stmt->execute([$accessKeyId]);
        $data = $stmt->fetchAll(PDO::FETCH_ASSOC);
        return array_reverse($data);
    }

    /**
     * 清理过期统计数据
     */
    public function pruneStats()
    {
        // 1. 清理小时表：保留最近 24+2 小时以外的数据
        // 既然我们只取 Limit 24，其实可以删掉 48 小时前的
        $hourLimit = time() - (48 * 3600);
        $this->pdo->exec("DELETE FROM traffic_hourly WHERE recorded_at < $hourLimit");

        // 2. 清理天表：保留最近 60 天以外的 (留点余量)
        $dayLimit = time() - (60 * 86400);
        $this->pdo->exec("DELETE FROM traffic_daily WHERE recorded_at < $dayLimit");
    }
}
