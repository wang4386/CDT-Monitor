<?php

require 'vendor/autoload.php';

use AlibabaCloud\Client\AlibabaCloud;
use AlibabaCloud\Client\Exception\ClientException;
use AlibabaCloud\Client\Exception\ServerException;
use PHPMailer\PHPMailer\PHPMailer;

class AliyunTrafficCheck
{
    private $db;
    private $dbFile; 
    private $initError = null;
    
    private $configCache = [];
    private $accountsCache = [];

    const API_INTERVAL = 600; 
    // 新增：保活冷却时间 (秒)，防止短时间内连续重启/发信
    const KEEP_ALIVE_COOLDOWN = 1800; // 30分钟

    public function __construct()
    {
        $this->dbFile = __DIR__ . '/data.sqlite';
        try {
            $this->connectDb();
            $this->initDb();
            $this->loadConfig();
        } catch (Exception $e) {
            $this->initError = $e->getMessage();
        }
    }

    public function getInitError() {
        return $this->initError;
    }

    private function connectDb()
    {
        try {
            $this->db = new PDO('sqlite:' . $this->dbFile);
            $this->db->setAttribute(PDO::ATTR_ERRMODE, PDO::ERRMODE_EXCEPTION);
            $this->db->setAttribute(PDO::ATTR_DEFAULT_FETCH_MODE, PDO::FETCH_ASSOC);
        } catch (PDOException $e) {
            if (strpos($e->getMessage(), 'unable to open database file') !== false) {
                $user = get_current_user();
                throw new Exception("权限不足：Web用户 ({$user}) 无法在目录 " . __DIR__ . " 中创建文件。<br>请尝试执行命令：chown -R {$user}:{$user} " . __DIR__);
            }
            throw new Exception("Database Error: " . $e->getMessage());
        }
    }

    private function initDb()
    {
        if ($this->initError) return;

        $this->db->exec("CREATE TABLE IF NOT EXISTS settings (
            key TEXT PRIMARY KEY,
            value TEXT
        )");

        $this->db->exec("CREATE TABLE IF NOT EXISTS accounts (
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

        $this->addColumnIfNotExists('accounts', 'traffic_used', 'REAL DEFAULT 0');
        $this->addColumnIfNotExists('accounts', 'instance_status', "TEXT DEFAULT 'Unknown'");
        $this->addColumnIfNotExists('accounts', 'updated_at', 'INTEGER DEFAULT 0');
        // 新增字段：上次保活时间
        $this->addColumnIfNotExists('accounts', 'last_keep_alive_at', 'INTEGER DEFAULT 0');
    }

    private function addColumnIfNotExists($table, $column, $definition)
    {
        try {
            $this->db->query("SELECT $column FROM $table LIMIT 1");
        } catch (Exception $e) {
            $this->db->exec("ALTER TABLE $table ADD COLUMN $column $definition");
        }
    }

    private function loadConfig()
    {
        if ($this->initError) return;
        
        $stmt = $this->db->query("SELECT key, value FROM settings");
        while ($row = $stmt->fetch()) {
            $this->configCache[$row['key']] = $row['value'];
        }

        $stmt = $this->db->query("SELECT * FROM accounts");
        $this->accountsCache = $stmt->fetchAll();
    }

    private function saveSetting($key, $value)
    {
        if ($this->initError) return;
        $stmt = $this->db->prepare("INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)");
        $stmt->execute([$key, $value]);
        $this->configCache[$key] = $value;
    }

    public function isInitialized()
    {
        if ($this->initError) return false;
        return isset($this->configCache['admin_password']) && !empty($this->configCache['admin_password']);
    }

    public function setup($data)
    {
        if ($this->initError) throw new Exception($this->initError);
        if ($this->isInitialized()) return false; 
        return $this->updateConfig($data);
    }

    public function getAdminPassword()
    {
        return $this->configCache['admin_password'] ?? '';
    }

    public function login($password)
    {
        $adminPass = $this->getAdminPassword();
        if (empty($adminPass)) return false;
        return (string)$password === (string)$adminPass;
    }

    public function updateConfig($data)
    {
        if ($this->initError) return false;
        try {
            $this->db->beginTransaction();

            $this->saveSetting('admin_password', $data['admin_password']);
            $this->saveSetting('traffic_threshold', $data['traffic_threshold']);
            $this->saveSetting('enable_schedule_email', $data['enable_schedule_email'] ? '1' : '0');
            $this->saveSetting('shutdown_mode', $data['shutdown_mode']);
            $this->saveSetting('threshold_action', $data['threshold_action']);
            $this->saveSetting('keep_alive', isset($data['keep_alive']) && $data['keep_alive'] ? '1' : '0');
            
            if (isset($data['Notification'])) {
                $this->saveSetting('notify_email', $data['Notification']['email']);
                $this->saveSetting('notify_host', $data['Notification']['host']);
                $this->saveSetting('notify_port', $data['Notification']['port']);
                $this->saveSetting('notify_username', $data['Notification']['username']);
                $this->saveSetting('notify_password', $data['Notification']['password']);
                $this->saveSetting('notify_secure', $data['Notification']['secure']);
            }

            $existingCache = [];
            $stmt = $this->db->query("SELECT access_key_id, traffic_used, instance_status, updated_at, last_keep_alive_at FROM accounts");
            while ($row = $stmt->fetch()) {
                $existingCache[$row['access_key_id']] = $row;
            }

            $this->db->exec("DELETE FROM accounts");
            $stmt = $this->db->prepare("INSERT INTO accounts (access_key_id, access_key_secret, region_id, instance_id, max_traffic, schedule_enabled, start_time, stop_time, traffic_used, instance_status, updated_at, last_keep_alive_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)");
            
            if (isset($data['Accounts']) && is_array($data['Accounts'])) {
                foreach ($data['Accounts'] as $acc) {
                    $key = $acc['AccessKeyId'];
                    $traffic = $existingCache[$key]['traffic_used'] ?? 0;
                    $status = $existingCache[$key]['instance_status'] ?? 'Unknown';
                    $updated = $existingCache[$key]['updated_at'] ?? 0;
                    $lastKeepAlive = $existingCache[$key]['last_keep_alive_at'] ?? 0;

                    $stmt->execute([
                        $acc['AccessKeyId'],
                        $acc['AccessKeySecret'],
                        $acc['regionId'],
                        $acc['instanceId'] ?? '',
                        $acc['maxTraffic'],
                        ($acc['schedule']['enabled'] ?? false) ? 1 : 0,
                        $acc['schedule']['startTime'] ?? '',
                        $acc['schedule']['stopTime'] ?? '',
                        $traffic,
                        $status,
                        $updated,
                        $lastKeepAlive
                    ]);
                }
            }

            $this->db->commit();
            $this->loadConfig();
            return true;
        } catch (Exception $e) {
            if ($this->db && $this->db->inTransaction()) $this->db->rollBack();
            return false;
        }
    }

    public function getConfigForFrontend()
    {
        if ($this->initError) return [];
        $config = [
            'admin_password' => $this->configCache['admin_password'] ?? '',
            'traffic_threshold' => (int)($this->configCache['traffic_threshold'] ?? 95),
            'enable_schedule_email' => ($this->configCache['enable_schedule_email'] ?? '0') === '1',
            'shutdown_mode' => $this->configCache['shutdown_mode'] ?? 'KeepCharging',
            'threshold_action' => $this->configCache['threshold_action'] ?? 'stop_and_notify',
            'keep_alive' => ($this->configCache['keep_alive'] ?? '0') === '1',
            'Notification' => [
                'email' => $this->configCache['notify_email'] ?? '',
                'host' => $this->configCache['notify_host'] ?? '',
                'port' => $this->configCache['notify_port'] ?? 465,
                'username' => $this->configCache['notify_username'] ?? '',
                'password' => $this->configCache['notify_password'] ?? '',
                'secure' => $this->configCache['notify_secure'] ?? 'ssl',
            ],
            'Accounts' => []
        ];

        foreach ($this->accountsCache as $row) {
            $config['Accounts'][] = [
                'AccessKeyId' => $row['access_key_id'],
                'AccessKeySecret' => $row['access_key_secret'],
                'regionId' => $row['region_id'],
                'instanceId' => $row['instance_id'],
                'maxTraffic' => (float)$row['max_traffic'],
                'schedule' => [
                    'enabled' => $row['schedule_enabled'] == 1,
                    'startTime' => $row['start_time'],
                    'stopTime' => $row['stop_time']
                ]
            ];
        }

        return $config;
    }

    private function isTimeInRange($current, $start, $end) {
        if (!$start || !$end) return false;
        if ($start < $end) {
            return $current >= $start && $current < $end;
        } else {
            return $current >= $start || $current < $end;
        }
    }

    public function monitor()
    {
        if ($this->initError) return "Error: " . $this->initError;
        
        $logs = [];
        $currentUserTime = date('H:i');
        $currentTime = time();
        $threshold = (int)($this->configCache['traffic_threshold'] ?? 95);
        $shutdownMode = $this->configCache['shutdown_mode'] ?? 'KeepCharging';
        $thresholdAction = $this->configCache['threshold_action'] ?? 'stop_and_notify';
        $keepAlive = ($this->configCache['keep_alive'] ?? '0') === '1';
        
        $updateStmt = $this->db->prepare("UPDATE accounts SET traffic_used = ?, instance_status = ?, updated_at = ? WHERE id = ?");
        // 准备更新保活时间的SQL
        $updateKeepAliveStmt = $this->db->prepare("UPDATE accounts SET last_keep_alive_at = ? WHERE id = ?");

        foreach ($this->accountsCache as $account) {
            $logPrefix = "[{$account['access_key_id']}]";
            $actions = [];
            $forceRefresh = false;

            // 1. 定时任务 (优先级最高)
            if ($account['schedule_enabled'] == 1) {
                if ($account['start_time'] && $currentUserTime === $account['start_time']) {
                    $this->controlInstance($account, 'start');
                    $actions[] = "定时启动";
                    $this->notifySchedule("启动", $account);
                    $forceRefresh = true;
                }
                if ($account['stop_time'] && $currentUserTime === $account['stop_time']) {
                    $this->controlInstance($account, 'stop', $shutdownMode);
                    $actions[] = "定时停止({$shutdownMode})";
                    $this->notifySchedule("停止", $account);
                    $forceRefresh = true;
                }
            }

            // 2. 数据获取
            $lastUpdate = $account['updated_at'] ?? 0;
            $cachedStatus = $account['instance_status'] ?? 'Unknown';
            $shouldCheckApi = $forceRefresh || (($currentTime - $lastUpdate) > self::API_INTERVAL) || ($cachedStatus === 'Unknown');
            
            // 关键逻辑：默认更新为当前时间，但如果失败则保持旧时间以便重试
            $newUpdateTime = $currentTime;

            if ($shouldCheckApi) {
                $newTraffic = $this->getTrafficApi($account['access_key_id'], $account['access_key_secret']);
                $status = $this->getInstanceStatusApi($account);
                
                if ($status === 'Unknown') {
                    usleep(500000); 
                    $status = $this->getInstanceStatusApi($account);
                }

                if ($newTraffic < 0) {
                    $traffic = $account['traffic_used']; 
                    $apiStatusLog = "流量API异常(保留旧值)";
                    // 失败：不更新时间戳，促使下次尽快重试
                    $newUpdateTime = $lastUpdate;
                } else {
                    $traffic = $newTraffic;
                    $apiStatusLog = "已更新API数据";
                }
                
                // 失败：如果不更新时间戳，促使下次尽快重试
                if ($status === 'Unknown') {
                    $newUpdateTime = $lastUpdate;
                    $apiStatusLog .= " [状态Unknown]";
                }

                $updateStmt->execute([$traffic, $status, $newUpdateTime, $account['id']]);
            } else {
                $traffic = $account['traffic_used'];
                $status = $account['instance_status'];
                $timeLeft = self::API_INTERVAL - ($currentTime - $lastUpdate);
                $apiStatusLog = "缓存有效({$timeLeft}s)";
            }

            $maxTraffic = $account['max_traffic'];
            $usagePercent = ($maxTraffic > 0) ? round(($traffic / $maxTraffic) * 100, 2) : 0;
            $trafficDesc = "流量:{$usagePercent}%";
            $isOverThreshold = $usagePercent >= $threshold;

            // 3. 流量阈值检查
            if ($isOverThreshold) {
                $trafficDesc .= "[警告]";
                if ($shouldCheckApi) {
                    if ($thresholdAction === 'stop_and_notify') {
                        if ($status !== 'Stopped') {
                            $this->controlInstance($account, 'stop', $shutdownMode);
                            $actions[] = "超限关机";
                            $status = 'Stopped';
                        }
                    } else {
                        $actions[] = "超限告警";
                    }
                    $this->sendNotification($account['access_key_id'], $traffic, $usagePercent, implode(',', $actions));
                }
            }

            // 4. 实例保活逻辑 (带冷却时间)
            if ($keepAlive && $account['schedule_enabled'] == 1 && !$isOverThreshold) {
                if ($this->isTimeInRange($currentUserTime, $account['start_time'], $account['stop_time'])) {
                    if ($status === 'Stopped') {
                        // 检查冷却时间
                        $lastKeepAlive = $account['last_keep_alive_at'] ?? 0;
                        $timeSinceLast = $currentTime - $lastKeepAlive;

                        if ($timeSinceLast > self::KEEP_ALIVE_COOLDOWN) {
                            // 执行保活
                            $this->controlInstance($account, 'start');
                            $actions[] = "保活启动";
                            $status = 'Starting';
                            $this->notifySchedule("保活启动", $account);
                            
                            // 立即更新保活时间戳
                            $updateKeepAliveStmt->execute([$currentTime, $account['id']]);
                        } else {
                            // 处于冷却期，跳过
                            $cooldownLeft = ceil((self::KEEP_ALIVE_COOLDOWN - $timeSinceLast) / 60);
                            $apiStatusLog .= " [保活冷却中: {$cooldownLeft}分]";
                        }
                    }
                }
            }

            $actionLog = empty($actions) ? "无动作" : implode(", ", $actions);
            $logs[] = sprintf("%s %s | %s | %s | %s", $logPrefix, $actionLog, $trafficDesc, $status, $apiStatusLog);
        }

        return implode(PHP_EOL, $logs);
    }

    public function getStatusForFrontend()
    {
        if ($this->initError) {
            return ['error' => $this->initError];
        }

        $data = [];
        $threshold = (int)($this->configCache['traffic_threshold'] ?? 95);
        $currentTime = time();
        $updateStmt = $this->db->prepare("UPDATE accounts SET traffic_used = ?, instance_status = ?, updated_at = ? WHERE id = ?");

        foreach ($this->accountsCache as $account) {
            $lastUpdate = $account['updated_at'] ?? 0;
            $cachedStatus = $account['instance_status'] ?? 'Unknown';
            $newUpdateTime = $currentTime;

            if (($currentTime - $lastUpdate) > self::API_INTERVAL || $cachedStatus === 'Unknown') {
                $newTraffic = $this->getTrafficApi($account['access_key_id'], $account['access_key_secret']);
                $status = $this->getInstanceStatusApi($account);
                
                if ($status === 'Unknown') {
                    usleep(500000); 
                    $status = $this->getInstanceStatusApi($account);
                }

                if ($newTraffic < 0) {
                    $traffic = $account['traffic_used']; 
                    $newUpdateTime = $lastUpdate; // 失败则不更新时间
                } else {
                    $traffic = $newTraffic;
                }
                
                if ($status === 'Unknown') {
                    $newUpdateTime = $lastUpdate; // 失败则不更新时间
                }

                $updateStmt->execute([$traffic, $status, $newUpdateTime, $account['id']]);
            } else {
                $traffic = $account['traffic_used'];
                $status = $account['instance_status'];
            }

            $usagePercent = ($account['max_traffic'] > 0) ? round(($traffic / $account['max_traffic']) * 100, 2) : 0;
            $isFull = $usagePercent >= $threshold;

            $data[] = [
                'account' => substr($account['access_key_id'], 0, 7) . '***',
                'flow_total' => (float)$account['max_traffic'],
                'flow_used' => round($traffic, 2),
                'percentageOfUse' => $usagePercent,
                'region' => $account['region_id'],
                'regionName' => $this->getRegionName($account['region_id']),
                'rate95' => $isFull,
                'threshold' => $threshold,
                'instanceStatus' => $status,
                'lastUpdated' => date('Y-m-d H:i:s', $lastUpdate > 0 ? $lastUpdate : $currentTime)
            ];
        }
        return ['data' => $data];
    }

    private function getTrafficApi($key, $secret)
    {
        try {
            AlibabaCloud::accessKeyClient($key, $secret)->regionId('cn-hongkong')->asDefaultClient();
            $result = AlibabaCloud::rpc()->product('CDT')->scheme('https')->version('2021-08-13')->action('ListCdtInternetTraffic')->method('POST')->host('cdt.aliyuncs.com')->request();
            if (isset($result['TrafficDetails'])) {
                return array_sum(array_column($result['TrafficDetails'], 'Traffic')) / (1024 * 1024 * 1024);
            }
            return -1;
        } catch (Exception $e) { 
            return -1; 
        }
    }

    private function getInstanceStatusApi($account) {
        try {
             AlibabaCloud::accessKeyClient($account['access_key_id'], $account['access_key_secret'])->regionId($account['region_id'])->asDefaultClient();
            $options = ['query' => ['RegionId' => $account['region_id']]];
            if (!empty($account['instance_id'])) $options['query']['InstanceId'] = $account['instance_id'];
            $result = AlibabaCloud::rpc()->product('Ecs')->scheme('https')->version('2014-05-26')->action('DescribeInstanceStatus')->method('POST')->host("ecs.{$account['region_id']}.aliyuncs.com")->options($options)->request();
            if (isset($result['InstanceStatuses']['InstanceStatus'][0]['Status'])) return $result['InstanceStatuses']['InstanceStatus'][0]['Status'];
        } catch (Exception $e) { return 'Unknown'; }
        return 'Unknown';
    }

    private function controlInstance($account, $action, $shutdownMode = 'KeepCharging')
    {
        try {
            AlibabaCloud::accessKeyClient($account['access_key_id'], $account['access_key_secret'])->regionId($account['region_id'])->asDefaultClient();
            if (empty($account['instance_id'])) return;
            $options = ['query' => ['RegionId' => $account['region_id'], 'InstanceId' => $account['instance_id']]];
            if ($action === 'stop') $options['query']['StoppedMode'] = $shutdownMode;
             AlibabaCloud::rpc()->product('Ecs')->scheme('https')->version('2014-05-26')->action($action === 'stop' ? 'StopInstance' : 'StartInstance')->method('POST')->host("ecs.{$account['region_id']}.aliyuncs.com")->options($options)->request();
        } catch (Exception $e) {}
    }
    
    private function notifySchedule($actionType, $account)
    {
        if (($this->configCache['enable_schedule_email'] ?? '0') !== '1') return;
        $msg = "账号 {$account['access_key_id']} 执行定时任务: {$actionType}";
        $this->send_mail($this->configCache['notify_email'], '', 'CDT定时任务通知', $msg);
    }

    private function sendNotification($accessKeyId, $traffic, $percentage, $statusText)
    {
        if (empty($this->configCache['notify_email'])) return;
        $threshold = $this->configCache['traffic_threshold'] ?? 95;
        $message = "账号: {$accessKeyId}<br>流量: {$traffic}GB<br>使用率: {$percentage}% (阈值: {$threshold}%)<br>状态: <b>{$statusText}</b>";
        $this->send_mail($this->configCache['notify_email'], '', 'CDT流量告警', $message);
    }

    public function sendTestEmail($to)
    {
        return $this->send_mail($to, 'Admin', 'CDT Monitor Test', '<h1>测试邮件</h1><p>配置正确。</p>');
    }

    private function send_mail($to, $name, $subject, $body)
    {
        $mail = new PHPMailer();
        $mail->CharSet = 'UTF-8';
        $mail->IsSMTP();
        $mail->SMTPDebug = 0;
        $mail->SMTPAuth = true;
        
        $secure = $this->configCache['notify_secure'] ?? 'ssl';
        if (!empty($secure)) {
            $mail->SMTPSecure = $secure; 
        } else {
            $mail->SMTPSecure = '';
            $mail->SMTPAutoTLS = false;
        }
        
        $mail->Host = $this->configCache['notify_host'] ?? '';
        $mail->Port = $this->configCache['notify_port'] ?? 465;
        $mail->Username = $this->configCache['notify_username'] ?? '';
        $mail->Password = $this->configCache['notify_password'] ?? '';
        
        $mail->SetFrom($mail->Username, '阿里云CDT监控');
        $mail->Subject = $subject;
        $mail->MsgHTML($body);
        $mail->AddAddress($to, $name);
        return $mail->Send() ? true : $mail->ErrorInfo;
    }

    private function getRegionName($regionId)
    {
        $regions = [
            'cn-hongkong' => '中国香港',
            'ap-southeast-1' => '新加坡',
            'us-west-1' => '美国(硅谷)',
            'us-east-1' => '美国(弗吉尼亚)',
            'cn-hangzhou' => '华东1(杭州)',
            'cn-shanghai' => '华东2(上海)',
            'cn-qingdao' => '华北1(青岛)',
            'cn-beijing' => '华北2(北京)',
            'cn-zhangjiakou' => '华北3(张家口)',
            'cn-huhehaote' => '华北5(呼和浩特)',
            'cn-wulanchabu' => '华北6(乌兰察布)',
            'cn-shenzhen' => '华南1(深圳)',
            'cn-heyuan' => '华南2(河源)',
            'cn-guangzhou' => '华南3(广州)',
            'cn-chengdu' => '西南1(成都)',
            'ap-northeast-1' => '日本(东京)',
        ];
        return $regions[$regionId] ?? $regionId;
    }

    public function renderTemplate() {
        return $this->renderPhpFile('template.html');
    }

    private function renderPhpFile($filePath)
    {
        if (!file_exists($filePath)) return "File not found";
        ob_start();
        include $filePath;
        return ob_get_clean();
    }
}