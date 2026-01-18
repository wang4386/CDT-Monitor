<?php

require 'vendor/autoload.php';
require_once 'Database.php';
require_once 'ConfigManager.php';
require_once 'AliyunService.php';
require_once 'NotificationService.php';

use AlibabaCloud\Client\Exception\ClientException;
use AlibabaCloud\Client\Exception\ServerException;

class AliyunTrafficCheck
{
    private $db;
    private $configManager;
    private $aliyunService;
    private $notificationService;
    private $initError = null;

    const KEEP_ALIVE_COOLDOWN = 1800; 

    public function __construct()
    {
        try {
            $this->db = new Database();
            $this->configManager = new ConfigManager($this->db);
            $this->aliyunService = new AliyunService();
            $this->notificationService = new NotificationService();
            
            // 注入配置到通知服务
            $this->notificationService->setConfig($this->configManager->getAllSettings());
            
        } catch (Exception $e) {
            $this->initError = $e->getMessage();
        }
    }

    public function getInitError()
    {
        return $this->initError;
    }

    public function isInitialized()
    {
        if ($this->initError) return false;
        return $this->configManager->isInitialized();
    }

    public function getAdminPassword()
    {
        return $this->configManager->get('admin_password', '');
    }

    public function login($password)
    {
        $adminPass = $this->getAdminPassword();
        if (empty($adminPass)) return false;
        return (string)$password === (string)$adminPass;
    }

    public function setup($data)
    {
        if ($this->initError) throw new Exception($this->initError);
        if ($this->isInitialized()) return false; 
        return $this->configManager->updateConfig($data);
    }

    public function updateConfig($data)
    {
        $success = $this->configManager->updateConfig($data);
        if ($success) {
            $this->notificationService->setConfig($this->configManager->getAllSettings());
        }
        return $success;
    }

    public function getConfigForFrontend()
    {
        if ($this->initError) return [];
        
        $settings = $this->configManager->getAllSettings();
        $accounts = $this->configManager->getAccounts();

        $config = [
            'admin_password' => $settings['admin_password'] ?? '',
            'traffic_threshold' => (int)($settings['traffic_threshold'] ?? 95),
            'enable_schedule_email' => ($settings['enable_schedule_email'] ?? '0') === '1',
            'shutdown_mode' => $settings['shutdown_mode'] ?? 'KeepCharging',
            'threshold_action' => $settings['threshold_action'] ?? 'stop_and_notify',
            'keep_alive' => ($settings['keep_alive'] ?? '0') === '1',
            'api_interval' => (int)($settings['api_interval'] ?? 600), 
            'Notification' => [
                'email' => $settings['notify_email'] ?? '',
                'host' => $settings['notify_host'] ?? '',
                'port' => $settings['notify_port'] ?? 465,
                'username' => $settings['notify_username'] ?? '',
                'password' => $settings['notify_password'] ?? '',
                'secure' => $settings['notify_secure'] ?? 'ssl',
            ],
            'Accounts' => []
        ];

        foreach ($accounts as $row) {
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

    public function getSystemLogs()
    {
        if ($this->initError) return [];
        $logs = $this->db->getLogs(50); 
        foreach ($logs as &$log) {
            $log['time_str'] = date('Y-m-d H:i:s', $log['created_at']);
        }
        return $logs;
    }

    // --- 核心监控逻辑 ---

    public function monitor()
    {
        if ($this->initError) return "Error: " . $this->initError;
        
        $this->db->pruneLogs(30); // 清理日志

        $logs = [];
        $currentUserTime = date('H:i');
        $currentTime = time();
        
        $threshold = (int)$this->configManager->get('traffic_threshold', 95);
        $shutdownMode = $this->configManager->get('shutdown_mode', 'KeepCharging');
        $thresholdAction = $this->configManager->get('threshold_action', 'stop_and_notify');
        $keepAlive = $this->configManager->get('keep_alive', '0') === '1';
        $userInterval = (int)$this->configManager->get('api_interval', 600);
        
        $accounts = $this->configManager->getAccounts();

        foreach ($accounts as $account) {
            $logPrefix = "[{$account['access_key_id']}]";
            $actions = [];
            $forceRefresh = false;
            $statusTransformed = false; 

            // 1. 定时任务
            if ($account['schedule_enabled'] == 1) {
                if ($account['start_time'] && $currentUserTime === $account['start_time']) {
                    if ($this->safeControlInstance($account, 'start')) {
                        $actions[] = "定时启动";
                        $this->db->addLog('info', "账号 {$account['access_key_id']} 触发定时启动");
                        $this->notificationService->notifySchedule("定时启动", $account, "计划任务已触发，实例正在启动。");
                        $forceRefresh = true;
                        $statusTransformed = true; 
                    }
                }
                if ($account['stop_time'] && $currentUserTime === $account['stop_time']) {
                    if ($this->safeControlInstance($account, 'stop', $shutdownMode)) {
                        $actions[] = "定时停止({$shutdownMode})";
                        $this->db->addLog('info', "账号 {$account['access_key_id']} 触发定时停止 ({$shutdownMode})");
                        $this->notificationService->notifySchedule("定时停止", $account, "计划任务已触发，实例已停止。");
                        $forceRefresh = true;
                        $statusTransformed = true;
                    }
                }
            }

            // 2. 自适应心跳
            $lastUpdate = $account['updated_at'] ?? 0;
            $cachedStatus = $account['instance_status'] ?? 'Unknown';
            $isTransientState = in_array($cachedStatus, ['Starting', 'Stopping', 'Pending', 'Unknown']);
            $currentInterval = ($isTransientState || $statusTransformed) ? 60 : $userInterval;

            $shouldCheckApi = $forceRefresh || (($currentTime - $lastUpdate) > $currentInterval);
            $newUpdateTime = $currentTime;

            if ($shouldCheckApi) {
                // 使用 Safe 方法
                $newTraffic = $this->safeGetTraffic($account);
                $status = $this->safeGetInstanceStatus($account);
                
                // 简单的重试逻辑交给 Safe 方法内部其实不太好做，这里保留上层重试逻辑
                if ($status === 'Unknown') {
                    usleep(500000); 
                    $status = $this->safeGetInstanceStatus($account);
                }

                if ($newTraffic < 0) {
                    $traffic = $account['traffic_used']; 
                    $apiStatusLog = "流量API异常";
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

                $this->configManager->updateAccountStatus($account['id'], $traffic, $status, $newUpdateTime);
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
                            if ($this->safeControlInstance($account, 'stop', $shutdownMode)) {
                                $actions[] = "超限关机";
                                $this->db->addLog('warning', "账号 {$account['access_key_id']} 流量超限 ({$usagePercent}%)，触发自动关机");
                                $this->configManager->updateAccountStatus($account['id'], $traffic, 'Stopping', $currentTime);
                                $status = 'Stopping';
                            }
                        }
                    } else {
                        $actions[] = "超限告警";
                        $this->db->addLog('warning', "账号 {$account['access_key_id']} 流量超限 ({$usagePercent}%)，仅发送告警");
                    }
                    $this->notificationService->sendTrafficWarning($account['access_key_id'], $traffic, $usagePercent, implode(',', $actions), $threshold);
                }
            }

            // 4. 保活逻辑
            if ($keepAlive && $account['schedule_enabled'] == 1 && !$isOverThreshold) {
                if ($this->isTimeInRange($currentUserTime, $account['start_time'], $account['stop_time'])) {
                    if ($status === 'Stopped') {
                        $lastKeepAlive = $account['last_keep_alive_at'] ?? 0;
                        $timeSinceLast = $currentTime - $lastKeepAlive;

                        if ($timeSinceLast > self::KEEP_ALIVE_COOLDOWN) {
                            if ($this->safeControlInstance($account, 'start')) {
                                $actions[] = "保活启动";
                                $this->db->addLog('info', "账号 {$account['access_key_id']} 触发保活启动");
                                $this->notificationService->notifySchedule("保活启动", $account, "检测到实例在工作时段非预期关机，已尝试自动启动。");
                                $this->configManager->updateLastKeepAlive($account['id'], $currentTime);
                                $this->configManager->updateAccountStatus($account['id'], $traffic, 'Starting', $currentTime);
                                $status = 'Starting';
                            }
                        } else {
                            $cooldownLeft = ceil((self::KEEP_ALIVE_COOLDOWN - $timeSinceLast) / 60);
                            $apiStatusLog .= " [保活冷却:{$cooldownLeft}m]";
                        }
                    }
                }
            }

            if ($statusTransformed) {
                $tempStatus = in_array("定时启动", $actions) ? 'Starting' : 'Stopping';
                $this->configManager->updateAccountStatus($account['id'], $traffic, $tempStatus, $currentTime);
                $apiStatusLog .= " -> 强制过渡态";
            }

            $actionLog = empty($actions) ? "无动作" : implode(", ", $actions);
            $logs[] = sprintf("%s %s | %s | %s | %s", $logPrefix, $actionLog, $trafficDesc, $status, $apiStatusLog);
        }
        
        // --- 新增：更新心跳时间 ---
        $this->configManager->updateLastRunTime(time());

        return implode(PHP_EOL, $logs);
    }

    public function getStatusForFrontend()
    {
        if ($this->initError) return ['error' => $this->initError];

        $data = [];
        $threshold = (int)$this->configManager->get('traffic_threshold', 95);
        $userInterval = (int)$this->configManager->get('api_interval', 600);
        
        $currentTime = time();
        $accounts = $this->configManager->getAccounts();

        foreach ($accounts as $account) {
            $lastUpdate = $account['updated_at'] ?? 0;
            $cachedStatus = $account['instance_status'] ?? 'Unknown';
            $newUpdateTime = $currentTime;

            $isTransientState = in_array($cachedStatus, ['Starting', 'Stopping', 'Pending', 'Unknown']);
            $checkInterval = $isTransientState ? 60 : $userInterval;

            if (($currentTime - $lastUpdate) > $checkInterval) {
                // 使用 Safe 方法
                $newTraffic = $this->safeGetTraffic($account);
                $status = $this->safeGetInstanceStatus($account);
                
                if ($status === 'Unknown') {
                    usleep(500000); 
                    $status = $this->safeGetInstanceStatus($account);
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

                $this->configManager->updateAccountStatus($account['id'], $traffic, $status, $newUpdateTime);
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
        
        // --- 新增：返回系统心跳时间 ---
        return [
            'data' => $data,
            'system_last_run' => $this->configManager->getLastRunTime()
        ];
    }

    public function refreshAccount($id)
    {
        if ($this->initError) return false;

        $targetAccount = $this->configManager->getAccountById($id);
        if (!$targetAccount) return false;

        $currentTime = time();
        // 使用 Safe 方法
        $traffic = $this->safeGetTraffic($targetAccount);
        $status = $this->safeGetInstanceStatus($targetAccount);

        if ($traffic < 0) {
            $traffic = $targetAccount['traffic_used']; 
        }

        return $this->configManager->updateAccountStatus($id, $traffic, $status, $currentTime);
    }

    public function sendTestEmail($to)
    {
        return $this->notificationService->sendTestEmail($to);
    }

    // --- 异常处理封装 (Safe Methods) ---

    private function safeGetTraffic($account)
    {
        try {
            return $this->aliyunService->getTraffic($account['access_key_id'], $account['access_key_secret']);
        } catch (ClientException $e) {
            $this->db->addLog('error', "流量查询配置错误 [{$account['access_key_id']}]: " . $e->getErrorMessage());
            return -1;
        } catch (ServerException $e) {
            $this->db->addLog('error', "流量查询服务错误 [{$account['access_key_id']}]: " . $e->getErrorMessage());
            return -1;
        } catch (\Exception $e) {
            $this->db->addLog('error', "流量查询未知异常 [{$account['access_key_id']}]: " . $e->getMessage());
            return -1;
        }
    }

    private function safeGetInstanceStatus($account)
    {
        try {
            return $this->aliyunService->getInstanceStatus($account);
        } catch (ClientException $e) {
            // 实例状态查询失败通常不应中断整个流程，记录日志并返回 Unknown
            $this->db->addLog('error', "实例查询配置错误 [{$account['access_key_id']}]: " . $e->getErrorMessage());
            return 'Unknown';
        } catch (ServerException $e) {
            $this->db->addLog('error', "实例查询服务错误 [{$account['access_key_id']}]: " . $e->getErrorMessage());
            return 'Unknown';
        } catch (\Exception $e) {
            return 'Unknown';
        }
    }

    private function safeControlInstance($account, $action, $shutdownMode = 'KeepCharging')
    {
        try {
            return $this->aliyunService->controlInstance($account, $action, $shutdownMode);
        } catch (ClientException $e) {
            $this->db->addLog('error', "实例操作配置错误 [{$account['access_key_id']} - {$action}]: " . $e->getErrorMessage());
            return false;
        } catch (ServerException $e) {
            $this->db->addLog('error', "实例操作服务错误 [{$account['access_key_id']} - {$action}]: " . $e->getErrorMessage());
            return false;
        } catch (\Exception $e) {
            $this->db->addLog('error', "实例操作失败 [{$account['access_key_id']} - {$action}]: " . $e->getMessage());
            return false;
        }
    }

    // --- 辅助方法 ---

    private function isTimeInRange($current, $start, $end) {
        if (!$start || !$end) return false;
        if ($start < $end) {
            return $current >= $start && $current < $end;
        } else {
            return $current >= $start || $current < $end;
        }
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
        if (!file_exists('template.html')) return "File not found";
        ob_start();
        include 'template.html';
        return ob_get_clean();
    }
}