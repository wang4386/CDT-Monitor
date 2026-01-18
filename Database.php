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

        // 新增：流量历史记录表
        // 注意：这里使用 access_key_id 作为关联键，因为 id 会因为 reorderIds 而改变
        $this->pdo->exec("CREATE TABLE IF NOT EXISTS traffic_history (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            access_key_id TEXT,
            traffic REAL,
            recorded_at INTEGER
        )");
        // 为 access_key_id 和 recorded_at 创建索引，加速查询
        $this->pdo->exec("CREATE INDEX IF NOT EXISTS idx_traffic_ak_time ON traffic_history (access_key_id, recorded_at)");

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

    // --- 流量历史记录相关方法 ---

    public function addTrafficHistory($accessKeyId, $traffic)
    {
        // 简单策略：直接插入。查询时再进行聚合。
        // 为了避免数据量过大，可以在插入前判断：如果最近 5 分钟内已有记录且流量未变，则跳过？
        // 这里为了图表平滑，我们选择全部记录（由 monitor 调用频率决定，通常每1-10分钟一次）
        $stmt = $this->pdo->prepare("INSERT INTO traffic_history (access_key_id, traffic, recorded_at) VALUES (?, ?, ?)");
        $stmt->execute([$accessKeyId, $traffic, time()]);
    }

    public function getTrafficHistory($accessKeyId, $startTime)
    {
        $stmt = $this->pdo->prepare("SELECT traffic, recorded_at FROM traffic_history WHERE access_key_id = ? AND recorded_at >= ? ORDER BY recorded_at ASC");
        $stmt->execute([$accessKeyId, $startTime]);
        return $stmt->fetchAll(PDO::FETCH_ASSOC);
    }

    public function pruneTrafficHistory($days = 31)
    {
        // 清理超过指定天数的历史记录
        $timestamp = time() - ($days * 86400);
        $stmt = $this->pdo->prepare("DELETE FROM traffic_history WHERE recorded_at < ?");
        $stmt->execute([$timestamp]);
    }
}