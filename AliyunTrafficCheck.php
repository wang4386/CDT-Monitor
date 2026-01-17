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

    // 默认保活冷却时间
    const KEEP_ALIVE_COOLDOWN = 1800; 

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
            $this->saveSetting('api_interval', $data['api_interval'] ?? 600);
            
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
            'api_interval' => (int)($this->configCache['api_interval'] ?? 600), 
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

    public function refreshAccount($id)
    {
        if ($this->initError) return false;

        $targetAccount = null;
        foreach ($this->accountsCache as $acc) {
            if ($acc['id'] == $id) {
                $targetAccount = $acc;
                break;
            }
        }

        if (!$targetAccount) return false;

        $currentTime = time();
        $updateStmt = $this->db->prepare("UPDATE accounts SET traffic_used = ?, instance_status = ?, updated_at = ? WHERE id = ?");

        $traffic = $this->getTrafficApi($targetAccount['access_key_id'], $targetAccount['access_key_secret']);
        $status = $this->getInstanceStatusApi($targetAccount);

        if ($traffic < 0) {
            $traffic = $targetAccount['traffic_used']; 
        }

        return $updateStmt->execute([$traffic, $status, $currentTime, $id]);
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
        
        $userInterval = (int)($this->configCache['api_interval'] ?? 600);
        
        $updateStmt = $this->db->prepare("UPDATE accounts SET traffic_used = ?, instance_status = ?, updated_at = ? WHERE id = ?");
        $updateKeepAliveStmt = $this->db->prepare("UPDATE accounts SET last_keep_alive_at = ? WHERE id = ?");

        foreach ($this->accountsCache as $account) {
            $logPrefix = "[{$account['access_key_id']}]";
            $actions = [];
            $forceRefresh = false;
            $statusTransformed = false; 

            // 1. 定时任务 (逻辑：触发 -> 强制刷新 -> 状态变更为过渡态)
            if ($account['schedule_enabled'] == 1) {
                if ($account['start_time'] && $currentUserTime === $account['start_time']) {
                    $this->controlInstance($account, 'start');
                    $actions[] = "定时启动";
                    $this->notifySchedule("定时启动", $account, "计划任务已触发，实例正在启动。");
                    $forceRefresh = true;
                    $statusTransformed = true; 
                }
                if ($account['stop_time'] && $currentUserTime === $account['stop_time']) {
                    $this->controlInstance($account, 'stop', $shutdownMode);
                    $actions[] = "定时停止({$shutdownMode})";
                    $this->notifySchedule("定时停止", $account, "计划任务已触发，实例已停止。");
                    $forceRefresh = true;
                    $statusTransformed = true;
                }
            }

            // 2. 自适应心跳机制 (Smart Burst)
            $lastUpdate = $account['updated_at'] ?? 0;
            $cachedStatus = $account['instance_status'] ?? 'Unknown';
            
            // 核心闭环：只要状态是“中间态”或“未知”，就强制每60秒检查一次
            // 即使阿里云 API 调用慢，只要返回的状态还是 Starting/Stopping，这里就会持续保持高频
            $isTransientState = in_array($cachedStatus, ['Starting', 'Stopping', 'Pending', 'Unknown']);
            $currentInterval = ($isTransientState || $statusTransformed) ? 60 : $userInterval;

            $shouldCheckApi = $forceRefresh || (($currentTime - $lastUpdate) > $currentInterval);
            
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
                    $apiStatusLog = "流量API异常(保留)";
                    $newUpdateTime = $lastUpdate;
                } else {
                    $traffic = $newTraffic;
                    $apiStatusLog = "已更新";
                }
                
                if ($status === 'Unknown') {
                    $newUpdateTime = $lastUpdate;
                    $apiStatusLog .= "(状态Unknown)";
                } else {
                    $apiStatusLog .= in_array($status, ['Starting', 'Stopping', 'Pending']) ? " [过渡态]" : " [稳定态]";
                }

                $updateStmt->execute([$traffic, $status, $newUpdateTime, $account['id']]);
            } else {
                $traffic = $account['traffic_used'];
                $status = $account['instance_status'];
                $timeLeft = $currentInterval - ($currentTime - $lastUpdate);
                $apiStatusLog = "缓存({$timeLeft}s)";
            }

            $maxTraffic = $account['max_traffic'];
            $usagePercent = ($maxTraffic > 0) ? round(($traffic / $maxTraffic) * 100, 2) : 0;
            $trafficDesc = "流量:{$usagePercent}%";
            $isOverThreshold = $usagePercent >= $threshold;

            // 3. 流量熔断
            if ($isOverThreshold) {
                $trafficDesc .= "[警告]";
                if ($shouldCheckApi) {
                    if ($thresholdAction === 'stop_and_notify') {
                        if ($status !== 'Stopped') {
                            $this->controlInstance($account, 'stop', $shutdownMode);
                            $actions[] = "超限关机";
                            // 立即更新数据库为 Stopping，确保下一分钟依然高频检查
                            $updateStmt->execute([$traffic, 'Stopping', $currentTime, $account['id']]);
                            $status = 'Stopping';
                        }
                    } else {
                        $actions[] = "超限告警";
                    }
                    $this->sendNotification($account['access_key_id'], $traffic, $usagePercent, implode(',', $actions));
                }
            }

            // 4. 保活逻辑
            if ($keepAlive && $account['schedule_enabled'] == 1 && !$isOverThreshold) {
                if ($this->isTimeInRange($currentUserTime, $account['start_time'], $account['stop_time'])) {
                    if ($status === 'Stopped') {
                        $lastKeepAlive = $account['last_keep_alive_at'] ?? 0;
                        $timeSinceLast = $currentTime - $lastKeepAlive;

                        if ($timeSinceLast > self::KEEP_ALIVE_COOLDOWN) {
                            $this->controlInstance($account, 'start');
                            $actions[] = "保活启动";
                            $this->notifySchedule("保活启动", $account, "检测到实例在工作时段非预期关机，已尝试自动启动。");
                            $updateKeepAliveStmt->execute([$currentTime, $account['id']]);
                            // 立即更新数据库为 Starting，确保下一分钟依然高频检查
                            $updateStmt->execute([$traffic, 'Starting', $currentTime, $account['id']]);
                            $status = 'Starting';
                        } else {
                            $cooldownLeft = ceil((self::KEEP_ALIVE_COOLDOWN - $timeSinceLast) / 60);
                            $apiStatusLog .= " [保活冷却:{$cooldownLeft}m]";
                        }
                    }
                }
            }

            // 补充逻辑：如果刚刚执行了定时任务，立即将数据库状态置为过渡态
            if ($statusTransformed) {
                $tempStatus = in_array("定时启动", $actions) ? 'Starting' : 'Stopping';
                $updateStmt->execute([$traffic, $tempStatus, $currentTime, $account['id']]);
                $apiStatusLog .= " -> 强制过渡态";
            }

            $actionLog = empty($actions) ? "无动作" : implode(", ", $actions);
            $logs[] = sprintf("%s %s | %s | %s | %s", $logPrefix, $actionLog, $trafficDesc, $status, $apiStatusLog);
        }

        return implode(PHP_EOL, $logs);
    }

    public function getStatusForFrontend()
    {
        if ($this->initError) return ['error' => $this->initError];

        $data = [];
        $threshold = (int)($this->configCache['traffic_threshold'] ?? 95);
        $userInterval = (int)($this->configCache['api_interval'] ?? 600);
        
        $currentTime = time();
        $updateStmt = $this->db->prepare("UPDATE accounts SET traffic_used = ?, instance_status = ?, updated_at = ? WHERE id = ?");

        foreach ($this->accountsCache as $account) {
            $lastUpdate = $account['updated_at'] ?? 0;
            $cachedStatus = $account['instance_status'] ?? 'Unknown';
            $newUpdateTime = $currentTime;

            // 前端自适应：如果数据库记录的是中间态，说明正在变动中，前端超时时间也缩短为60秒
            $isTransientState = in_array($cachedStatus, ['Starting', 'Stopping', 'Pending', 'Unknown']);
            $checkInterval = $isTransientState ? 60 : $userInterval;

            if (($currentTime - $lastUpdate) > $checkInterval) {
                $newTraffic = $this->getTrafficApi($account['access_key_id'], $account['access_key_secret']);
                $status = $this->getInstanceStatusApi($account);
                
                if ($status === 'Unknown') {
                    usleep(500000); 
                    $status = $this->getInstanceStatusApi($account);
                }

                if ($newTraffic < 0) {
                    $traffic = $account['traffic_used']; 
                    $newUpdateTime = $lastUpdate;
                } else {
                    $traffic = $newTraffic;
                }
                
                if ($status === 'Unknown') {
                    $newUpdateTime = $lastUpdate;
                }

                $updateStmt->execute([$traffic, $status, $newUpdateTime, $account['id']]);
            } else {
                $traffic = $account['traffic_used'];
                $status = $account['instance_status'];
            }

            $usagePercent = ($account['max_traffic'] > 0) ? round(($traffic / $account['max_traffic']) * 100, 2) : 0;
            $isFull = $usagePercent >= $threshold;

            $data[] = [
                'id' => $account['id'], 
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
        } catch (Exception $e) { return -1; }
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
    
    // ... (邮件部分代码保持不变，为节省篇幅省略) ...
    // 请确保文件包含完整的 renderEmailTemplate, send_mail 等方法
    private function notifySchedule($actionType, $account, $description = "")
    {
        if (($this->configCache['enable_schedule_email'] ?? '0') !== '1') return;
        $title = "定时任务: " . $actionType;
        $maskedKey = substr($account['access_key_id'], 0, 7) . '***';
        $details = [
            ['label' => '账号 ID', 'value' => $maskedKey],
            ['label' => '执行动作', 'value' => $actionType, 'highlight' => true],
            ['label' => '执行时间', 'value' => date('Y-m-d H:i:s')],
            ['label' => '详情说明', 'value' => $description ?: '根据预设时间表自动执行。']
        ];
        $html = $this->renderEmailTemplate($title, "您的实例已执行{$actionType}操作", $details, 'info');
        $this->send_mail($this->configCache['notify_email'], '', "CDT通知 - {$actionType}", $html);
    }

    private function sendNotification($accessKeyId, $traffic, $percentage, $statusText)
    {
        if (empty($this->configCache['notify_email'])) return;
        $threshold = $this->configCache['traffic_threshold'] ?? 95;
        $title = "流量告警 - " . $statusText;
        $details = [
            ['label' => '账号 ID', 'value' => substr($accessKeyId, 0, 7) . '***'],
            ['label' => '当前流量', 'value' => $traffic . ' GB'],
            ['label' => '使用率', 'value' => $percentage . '%', 'highlight' => true],
            ['label' => '设定阈值', 'value' => $threshold . '%'],
            ['label' => '当前状态', 'value' => $statusText]
        ];
        $html = $this->renderEmailTemplate($title, "检测到流量异常或达到阈值", $details, 'warning');
        $this->send_mail($this->configCache['notify_email'], '', 'CDT流量熔断告警', $html);
    }

    public function sendTestEmail($to)
    {
        $details = [
            ['label' => '测试结果', 'value' => '成功 (Success)'],
            ['label' => '发送时间', 'value' => date('Y-m-d H:i:s')],
            ['label' => '服务器', 'value' => $_SERVER['SERVER_NAME'] ?? 'localhost']
        ];
        $html = $this->renderEmailTemplate("测试邮件", "SMTP 配置验证成功", $details, 'success');
        return $this->send_mail($to, 'Admin', 'CDT Monitor Test', $html);
    }

    private function renderEmailTemplate($title, $summary, $details, $type = 'info')
    {
        $color = '#007AFF'; 
        if ($type === 'warning') $color = '#FF3B30'; 
        if ($type === 'success') $color = '#34C759'; 

        $rows = '';
        foreach ($details as $item) {
            $valColor = isset($item['highlight']) && $item['highlight'] ? $color : '#1C1C1E';
            $rows .= "
            <tr style='border-bottom: 1px solid #F2F2F7;'>
                <td style='padding: 12px 0; color: #8E8E93; font-size: 14px; width: 40%;'>{$item['label']}</td>
                <td style='padding: 12px 0; color: {$valColor}; font-size: 14px; font-weight: 600; text-align: right;'>{$item['value']}</td>
            </tr>";
        }

        return "
        <!DOCTYPE html>
        <html>
        <head><meta charset='utf-8'></head>
        <body style='margin: 0; padding: 0; background-color: #F2F2F7; font-family: sans-serif;'>
            <table width='100%' border='0' cellspacing='0' cellpadding='0'>
                <tr><td align='center' style='padding: 40px 20px;'>
                    <table width='100%' border='0' cellspacing='0' cellpadding='0' style='max-width: 500px; background-color: #FFFFFF; border-radius: 24px; overflow: hidden;'>
                        <tr><td style='height: 6px; background-color: {$color};'></td></tr>
                        <tr><td style='padding: 40px 30px;'>
                            <div style='color: {$color}; font-size: 12px; font-weight: 700; text-transform: uppercase; margin-bottom: 8px;'>CDT MONITOR</div>
                            <h1 style='margin: 0 0 10px 0; font-size: 24px; color: #1C1C1E;'>{$title}</h1>
                            <p style='margin: 0 0 30px 0; font-size: 15px; color: #8E8E93;'>{$summary}</p>
                            <table width='100%' border='0' cellspacing='0' cellpadding='0' style='border-top: 1px solid #F2F2F7;'>{$rows}</table>
                        </td></tr>
                        <tr><td style='background-color: #FAFAFC; padding: 20px; text-align: center; color: #AEAEB2; font-size: 12px;'>&copy; " . date('Y') . " CDT Monitor</td></tr>
                    </table>
                </td></tr>
            </table>
        </body></html>";
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