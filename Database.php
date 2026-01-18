<?php

class Database
{
    private $pdo;
    private $dbFile;

    public function __construct($dbFile = null)
    {
        // 变更 1: 默认路径修改为 /data/ 子目录
        $this->dbFile = $dbFile ?: __DIR__ . '/data/data.sqlite';
        
        // 变更 2: 在连接前执行环境安全检查与初始化
        $this->secureEnvironment();
        
        $this->connect();
        $this->initSchema();
    }

    /**
     * 安全环境初始化
     * 1. 创建独立数据目录
     * 2. 自动迁移旧数据（如果存在）
     * 3. 部署 .htaccess 防火墙
     * 4. 部署防目录遍历空文件
     */
    private function secureEnvironment()
    {
        $dir = dirname($this->dbFile);
        $oldFile = __DIR__ . '/data.sqlite'; // 旧文件位置

        // 1. 自动创建目录
        if (!is_dir($dir)) {
            if (!@mkdir($dir, 0755, true)) {
                $this->throwPermissionError($dir);
            }
        }

        // 2. 自动迁移旧数据 (如果根目录有旧数据，且新目录没有数据)
        if (file_exists($oldFile) && !file_exists($this->dbFile)) {
            if (!@rename($oldFile, $this->dbFile)) {
                // 如果移动失败（通常是权限问题），尝试复制
                if (@copy($oldFile, $this->dbFile)) {
                    @unlink($oldFile); // 复制成功后尝试删除旧文件
                } else {
                    // 如果都失败了，抛出权限异常，提示用户手动移动
                    throw new Exception("安全迁移失败：无法将旧数据库 ($oldFile) 移动到新目录 ($dir)。<br>请手动移动文件或检查目录权限。");
                }
            }
        }

        // 3. 部署 Apache/LiteSpeed 防火墙规则
        $htaccess = $dir . '/.htaccess';
        if (!file_exists($htaccess)) {
            $content = "Order Deny,Allow\nDeny from all";
            @file_put_contents($htaccess, $content);
        }

        // 4. 部署防目录遍历诱饵
        $indexHtml = $dir . '/index.html';
        if (!file_exists($indexHtml)) {
            @file_put_contents($indexHtml, ''); // 空文件即可
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
        throw new Exception("权限不足：Web用户 ({$user}) 无法在目录 {$dir} 中读写文件。<br>请在服务器终端执行命令修复权限：<br><code>chown -R {$user}:{$user} " . __DIR__ . "</code>");
    }

    private function initSchema()
    {
        $this->pdo->exec("CREATE TABLE IF NOT EXISTS settings (
            key TEXT PRIMARY KEY,
            value TEXT
        )");

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

        $this->pdo->exec("CREATE TABLE IF NOT EXISTS logs (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            type TEXT,
            message TEXT,
            created_at INTEGER
        )");

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
        $timestamp = time() - ($days * 86400);
        $stmt = $this->pdo->prepare("DELETE FROM logs WHERE created_at < ?");
        $stmt->execute([$timestamp]);
    }
}