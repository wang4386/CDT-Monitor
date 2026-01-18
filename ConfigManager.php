<?php

class ConfigManager
{
    private $db;
    private $configCache = [];
    private $accountsCache = [];

    public function __construct(Database $db)
    {
        $this->db = $db->getPdo();
        $this->load();
    }

    public function load()
    {
        $stmt = $this->db->query("SELECT key, value FROM settings");
        while ($row = $stmt->fetch()) {
            $this->configCache[$row['key']] = $row['value'];
        }

        $stmt = $this->db->query("SELECT * FROM accounts ORDER BY id ASC");
        $this->accountsCache = $stmt->fetchAll();
    }

    public function get($key, $default = null)
    {
        return $this->configCache[$key] ?? $default;
    }

    public function getAllSettings()
    {
        return $this->configCache;
    }

    public function getAccounts()
    {
        return $this->accountsCache;
    }

    public function getAccountById($id)
    {
        foreach ($this->accountsCache as $acc) {
            if ($acc['id'] == $id) return $acc;
        }
        return null;
    }

    public function isInitialized()
    {
        return !empty($this->configCache['admin_password']);
    }

    private function saveSetting($key, $value)
    {
        $stmt = $this->db->prepare("INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)");
        $stmt->execute([$key, $value]);
        $this->configCache[$key] = $value;
    }

    // --- 新增：心跳时间管理 ---
    
    public function updateLastRunTime($time)
    {
        $this->saveSetting('last_monitor_run', $time);
    }

    public function getLastRunTime()
    {
        return (int)($this->configCache['last_monitor_run'] ?? 0);
    }

    // ------------------------

    public function updateConfig($data)
    {
        try {
            $this->db->beginTransaction();

            // 1. 保存全局设置
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

            // 2. 账号增量同步
            $newAccounts = $data['Accounts'] ?? [];
            $stmt = $this->db->query("SELECT id, access_key_id FROM accounts");
            $existingMap = []; 
            while ($row = $stmt->fetch()) {
                $existingMap[$row['access_key_id']] = $row['id'];
            }
            
            $keptIds = [];
            $insertStmt = $this->db->prepare("INSERT INTO accounts (access_key_id, access_key_secret, region_id, instance_id, max_traffic, schedule_enabled, start_time, stop_time, traffic_used, instance_status, updated_at, last_keep_alive_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, 'Unknown', 0, 0)");
            $updateStmt = $this->db->prepare("UPDATE accounts SET access_key_secret = ?, region_id = ?, instance_id = ?, max_traffic = ?, schedule_enabled = ?, start_time = ?, stop_time = ? WHERE id = ?");

            foreach ($newAccounts as $acc) {
                $key = $acc['AccessKeyId'];
                $params = [
                    $acc['AccessKeySecret'], $acc['regionId'], $acc['instanceId'] ?? '', $acc['maxTraffic'],
                    ($acc['schedule']['enabled'] ?? false) ? 1 : 0,
                    $acc['schedule']['startTime'] ?? '', $acc['schedule']['stopTime'] ?? ''
                ];

                if (isset($existingMap[$key])) {
                    $id = $existingMap[$key];
                    $params[] = $id;
                    $updateStmt->execute($params);
                    $keptIds[] = $id;
                } else {
                    $insertParams = [$key];
                    array_push($insertParams, ...$params);
                    $insertStmt->execute($insertParams);
                }
            }

            // 3. 删除移除的账号
            $idsToDelete = array_diff(array_values($existingMap), $keptIds);
            if (!empty($idsToDelete)) {
                $placeholders = implode(',', array_fill(0, count($idsToDelete), '?'));
                $deleteStmt = $this->db->prepare("DELETE FROM accounts WHERE id IN ($placeholders)");
                $deleteStmt->execute($idsToDelete);
            }

            $this->db->commit();
            
            // 4. 重排 ID
            $this->reorderIds();

            // 5. 刷新缓存
            $this->load();
            return true;
        } catch (Exception $e) {
            if ($this->db->inTransaction()) $this->db->rollBack();
            return false;
        }
    }

    private function reorderIds()
    {
        try {
            $this->db->beginTransaction();
            $stmt = $this->db->query("SELECT * FROM accounts ORDER BY id ASC");
            $rows = $stmt->fetchAll(PDO::FETCH_ASSOC);

            if (!empty($rows)) {
                $this->db->exec("DELETE FROM accounts");
                $this->db->exec("DELETE FROM sqlite_sequence WHERE name='accounts'");
                
                $insertStmt = $this->db->prepare("INSERT INTO accounts (id, access_key_id, access_key_secret, region_id, instance_id, max_traffic, schedule_enabled, start_time, stop_time, traffic_used, instance_status, updated_at, last_keep_alive_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)");
                
                $newId = 1;
                foreach ($rows as $row) {
                    $insertStmt->execute([
                        $newId++, $row['access_key_id'], $row['access_key_secret'], $row['region_id'],
                        $row['instance_id'], $row['max_traffic'], $row['schedule_enabled'],
                        $row['start_time'], $row['stop_time'], $row['traffic_used'],
                        $row['instance_status'], $row['updated_at'], $row['last_keep_alive_at']
                    ]);
                }
            }
            $this->db->commit();
        } catch (Exception $e) {
            if ($this->db->inTransaction()) $this->db->rollBack();
        }
    }

    public function updateAccountStatus($id, $traffic, $status, $updatedAt)
    {
        $stmt = $this->db->prepare("UPDATE accounts SET traffic_used = ?, instance_status = ?, updated_at = ? WHERE id = ?");
        return $stmt->execute([$traffic, $status, $updatedAt, $id]);
    }

    public function updateLastKeepAlive($id, $time)
    {
        $stmt = $this->db->prepare("UPDATE accounts SET last_keep_alive_at = ? WHERE id = ?");
        return $stmt->execute([$time, $id]);
    }
}