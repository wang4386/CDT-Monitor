<?php

use PHPMailer\PHPMailer\PHPMailer;

class NotificationService
{
    private $config;

    public function setConfig($config)
    {
        $this->config = $config;
    }

    /**
     * 发送定时任务通知
     * @return bool|string 成功返回 true，失败返回错误信息
     */
    public function notifySchedule($actionType, $account, $description = "")
    {
        if (($this->config['enable_schedule_email'] ?? '0') !== '1') return true; 
        
        $title = "定时任务: " . $actionType;
        $maskedKey = substr($account['access_key_id'], 0, 7) . '***';
        $details = [
            ['label' => '账号 ID', 'value' => $maskedKey],
            ['label' => '执行动作', 'value' => $actionType, 'highlight' => true],
            ['label' => '执行时间', 'value' => date('Y-m-d H:i:s')],
            ['label' => '详情说明', 'value' => $description ?: '根据预设时间表自动执行。']
        ];
        
        $textMsg = "【CDT Monitor】{$title}\n" .
                   "账号 ID: {$maskedKey}\n" .
                   "执行动作: {$actionType}\n" .
                   "执行时间: " . date('Y-m-d H:i:s') . "\n" .
                   "详情说明: " . ($description ?: '根据预设时间表自动执行。');

        return $this->dispatchNotifications($title, "您的实例已执行{$actionType}操作", $details, 'info', $textMsg, $account['access_key_id']);
    }

    /**
     * 发送流量告警
     * @return bool|string
     */
    public function sendTrafficWarning($accessKeyId, $traffic, $percentage, $statusText, $threshold)
    {
        $title = "流量告警 - " . $statusText;
        $details = [
            ['label' => '账号 ID', 'value' => substr($accessKeyId, 0, 7) . '***'],
            ['label' => '当前流量', 'value' => $traffic . ' GB'],
            ['label' => '使用率', 'value' => $percentage . '%', 'highlight' => true],
            ['label' => '设定阈值', 'value' => $threshold . '%'],
            ['label' => '当前状态', 'value' => $statusText]
        ];

        $textMsg = "【CDT Monitor】{$title}\n" .
                   "账号 ID: " . substr($accessKeyId, 0, 7) . '***' . "\n" .
                   "当前流量: {$traffic} GB\n" .
                   "使用率: {$percentage}%\n" .
                   "设定阈值: {$threshold}%\n" .
                   "当前状态: {$statusText}";

        return $this->dispatchNotifications($title, "检测到流量异常或达到阈值", $details, 'warning', $textMsg, $accessKeyId);
    }

    public function sendTestEmail($to)
    {
        $details = [
            ['label' => '测试结果', 'value' => '成功 (Success)'],
            ['label' => '发送时间', 'value' => date('Y-m-d H:i:s')],
            ['label' => '服务器', 'value' => $_SERVER['SERVER_NAME'] ?? 'localhost']
        ];
        $html = $this->renderEmailTemplate("测试邮件", "SMTP 配置验证成功", $details, 'success');
        return $this->sendMail($to, 'Admin', 'CDT Monitor Test', $html);
    }

    public function sendTestTelegram($data)
    {
        $textMsg = "【CDT Monitor】测试推送\n这是一条来自 Telegram 的测试消息。\n发送时间: " . date('Y-m-d H:i:s');
        return $this->sendTelegram($textMsg, $data);
    }
    
    public function sendTestWebhook($data)
    {
        $textMsg = "【CDT Monitor】测试推送\n这是一条来自 Webhook 的测试消息。\n发送时间: " . date('Y-m-d H:i:s');
        $details = [['label' => '当前流量', 'value' => '0 GB']];
        return $this->sendWebhook($textMsg, "测试推送", $details, 'test_account_id', $data);
    }

    private function dispatchNotifications($title, $summary, $details, $type, $textMsg, $accountId = '')
    {
        $errors = [];
        $successCount = 0;
        $attemptCount = 0;

        // Email
        if (($this->config['notify_email_enabled'] ?? '1') === '1' && !empty($this->config['notify_email'])) {
            $attemptCount++;
            $html = $this->renderEmailTemplate($title, $summary, $details, $type);
            $res = $this->sendMail($this->config['notify_email'], '', "CDT通知 - " . $title, $html);
            if ($res === true) $successCount++;
            else $errors[] = "Email: " . $res;
        }

        // Telegram
        if (($this->config['notify_tg_enabled'] ?? '0') === '1' && !empty($this->config['notify_tg_token']) && !empty($this->config['notify_tg_chat_id'])) {
            $attemptCount++;
            $res = $this->sendTelegram($textMsg);
            if ($res === true) $successCount++;
            else $errors[] = "TG: " . $res;
        }

        // Webhook
        if (($this->config['notify_wh_enabled'] ?? '0') === '1' && !empty($this->config['notify_wh_url'])) {
            $attemptCount++;
            $res = $this->sendWebhook($textMsg, $title, $details, $accountId);
            if ($res === true) $successCount++;
            else $errors[] = "WH: " . $res;
        }

        if ($attemptCount == 0) return true; // No notifications enabled

        if ($successCount == 0 && count($errors) > 0) {
            return implode(" | ", $errors);
        } else if (count($errors) > 0) {
            return "部分完成: " . implode(" | ", $errors);
        }
        return true;
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

    private function sendMail($to, $name, $subject, $body)
    {
        $mail = new PHPMailer();
        $mail->CharSet = 'UTF-8';
        $mail->IsSMTP();
        $mail->SMTPDebug = 0;
        $mail->SMTPAuth = true;
        
        $secure = $this->config['notify_secure'] ?? 'ssl';
        if (!empty($secure)) {
            $mail->SMTPSecure = $secure; 
        } else {
            $mail->SMTPSecure = '';
            $mail->SMTPAutoTLS = false;
        }
        
        $mail->Host = $this->config['notify_host'] ?? '';
        $mail->Port = $this->config['notify_port'] ?? 465;
        $mail->Username = $this->config['notify_username'] ?? '';
        $mail->Password = $this->config['notify_password'] ?? '';
        
        $mail->SetFrom($mail->Username, '阿里云CDT监控');
        $mail->Subject = $subject;
        $mail->MsgHTML($body);
        $mail->AddAddress($to, $name);
        
        // 修改：返回 true 或 错误信息字符串
        if ($mail->Send()) {
            return true;
        } else {
            return $mail->ErrorInfo;
        }
    }

    private function sendTelegram($text, $overrideConfig = null)
    {
        $token = $overrideConfig['token'] ?? $this->config['notify_tg_token'] ?? '';
        $chatId = $overrideConfig['chat_id'] ?? $this->config['notify_tg_chat_id'] ?? '';
        $proxyType = $overrideConfig['proxy_type'] ?? $this->config['notify_tg_proxy_type'] ?? 'none';
        
        if (empty($token) || empty($chatId)) return "Telegram Token 或 Chat ID 为空";

        $url = "https://api.telegram.org/bot{$token}/sendMessage";
        if ($proxyType === 'custom' && !empty($overrideConfig['proxy_url'] ?? $this->config['notify_tg_proxy_url'] ?? '')) {
            $baseUrl = rtrim($overrideConfig['proxy_url'] ?? $this->config['notify_tg_proxy_url'], '/');
            $url = "{$baseUrl}/bot{$token}/sendMessage";
        }

        $postFields = [
            'chat_id' => $chatId,
            'text' => $text
        ];

        $ch = curl_init();
        curl_setopt($ch, CURLOPT_URL, $url);
        curl_setopt($ch, CURLOPT_POST, true);
        curl_setopt($ch, CURLOPT_POSTFIELDS, http_build_query($postFields));
        curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
        curl_setopt($ch, CURLOPT_TIMEOUT, 10);

        if ($proxyType === 'socks5') {
            $proxyIp = $overrideConfig['proxy_ip'] ?? $this->config['notify_tg_proxy_ip'] ?? '';
            $proxyPort = $overrideConfig['proxy_port'] ?? $this->config['notify_tg_proxy_port'] ?? '';
            if ($proxyIp && $proxyPort) {
                curl_setopt($ch, CURLOPT_PROXYTYPE, CURLPROXY_SOCKS5_HOSTNAME);
                curl_setopt($ch, CURLOPT_PROXY, "{$proxyIp}:{$proxyPort}");
                $proxyUser = $overrideConfig['proxy_user'] ?? $this->config['notify_tg_proxy_user'] ?? '';
                $proxyPass = $overrideConfig['proxy_pass'] ?? $this->config['notify_tg_proxy_pass'] ?? '';
                if ($proxyUser || $proxyPass) {
                    curl_setopt($ch, CURLOPT_PROXYUSERPWD, "{$proxyUser}:{$proxyPass}");
                }
            }
        }

        $result = curl_exec($ch);
        $error = curl_error($ch);
        $httpCode = curl_getinfo($ch, CURLINFO_HTTP_CODE);
        curl_close($ch);

        if ($error) return "Curl Error: " . $error;
        if ($httpCode != 200) return "HTTP Error {$httpCode}: " . $result;
        return true;
    }

    private function sendWebhook($text, $title, $details, $accountId = '', $overrideConfig = null)
    {
        $url = $overrideConfig['url'] ?? $this->config['notify_wh_url'] ?? '';
        $method = strtoupper($overrideConfig['method'] ?? $this->config['notify_wh_method'] ?? 'GET');
        $requestType = strtoupper($overrideConfig['request_type'] ?? $this->config['notify_wh_request_type'] ?? 'JSON');
        $headersStr = $overrideConfig['headers'] ?? $this->config['notify_wh_headers'] ?? '';
        $bodyTemplate = $overrideConfig['body'] ?? $this->config['notify_wh_body'] ?? '';

        if (empty($url)) return "Webhook URL为空";

        // Parse variables
        $traffic = 'N/A';
        $maxTraffic = 'N/A';
        foreach ($details as $d) {
            if ($d['label'] === '当前流量') $traffic = str_replace(' GB', '', $d['value']);
            if ($d['label'] === '设定阈值') $maxTraffic = str_replace('%', '', $d['value']);
        }
        $replacePairs = [
            '#TITLE#' => $title,
            '#MSG#' => $text,
            '#ACCOUNT#' => $accountId,
            '#TRAFFIC#' => $traffic,
            '#MAX_TRAFFIC#' => $maxTraffic
        ];

        $ch = curl_init();
        $customHeaders = [];

        // Parse custom headers
        if (!empty($headersStr)) {
            $parsedHeaders = json_decode($headersStr, true);
            if (is_array($parsedHeaders)) {
                foreach ($parsedHeaders as $k => $v) {
                    $customHeaders[] = "{$k}: {$v}";
                }
            }
        }

        if ($method === 'GET') {
            $urlReplacePairs = [];
            foreach ($replacePairs as $k => $v) {
                $urlReplacePairs[$k] = urlencode((string)$v);
            }
            $finalUrl = strtr($url, $urlReplacePairs);
            
            // If body template exists, replace vars and append it to URL query string if no URL vars were found. 
            // Fallback for simple GET without body:
            if (empty($bodyTemplate) && strpos($finalUrl, '?') === false && strpos($url, '#') === false) {
                // Default fallback if no variables exist in URL or body
                $payload = [
                    'title' => $title,
                    'text' => $text,
                    'time' => date('Y-m-d H:i:s')
                ];
                $finalUrl .= '?' . http_build_query($payload);
            }
            curl_setopt($ch, CURLOPT_URL, $finalUrl);
        } else {
            // POST request
            $urlReplacePairs = [];
            foreach ($replacePairs as $k => $v) {
                $urlReplacePairs[$k] = urlencode((string)$v);
            }
            curl_setopt($ch, CURLOPT_URL, strtr($url, $urlReplacePairs));
            curl_setopt($ch, CURLOPT_POST, true);
            
            $finalBody = '';
            if (!empty($bodyTemplate)) {
                $bodyReplacePairs = $replacePairs;
                if ($requestType === 'JSON') {
                    foreach ($bodyReplacePairs as $k => $v) {
                        // Safe JSON encoding for values injected into string literals
                        $bodyReplacePairs[$k] = substr(json_encode((string)$v, JSON_UNESCAPED_UNICODE), 1, -1);
                    }
                } else if ($requestType === 'FORM') {
                    foreach ($bodyReplacePairs as $k => $v) {
                        $bodyReplacePairs[$k] = urlencode((string)$v);
                    }
                }
                $finalBody = strtr($bodyTemplate, $bodyReplacePairs);
                
                // Content Type
                if ($requestType === 'JSON') {
                    $customHeaders[] = 'Content-Type: application/json';
                } else if ($requestType === 'FORM') {
                    $customHeaders[] = 'Content-Type: application/x-www-form-urlencoded';
                    // Test if user provided JSON instead of form data, attempt conversion
                    $decoded = json_decode($finalBody, true);
                    if (is_array($decoded)) {
                        $finalBody = http_build_query($decoded);
                    }
                }
            } else {
                // Fallback default payload if no body is configured
                $payload = ['title' => $title, 'text' => $text, 'time' => date('Y-m-d H:i:s')];
                if ($requestType === 'JSON') {
                    $finalBody = json_encode($payload, JSON_UNESCAPED_UNICODE);
                    $customHeaders[] = 'Content-Type: application/json';
                } else {
                    $finalBody = http_build_query($payload);
                    $customHeaders[] = 'Content-Type: application/x-www-form-urlencoded';
                }
            }
            curl_setopt($ch, CURLOPT_POSTFIELDS, $finalBody);
        }

        if (!empty($customHeaders)) {
            curl_setopt($ch, CURLOPT_HTTPHEADER, array_unique($customHeaders));
        }

        curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
        curl_setopt($ch, CURLOPT_TIMEOUT, 10);

        $result = curl_exec($ch);
        $error = curl_error($ch);
        $httpCode = curl_getinfo($ch, CURLINFO_HTTP_CODE);
        curl_close($ch);

        if ($error) return "Curl Error: " . $error;
        if ($httpCode >= 400) return "HTTP Error {$httpCode}: " . $result;
        return true;
    }
}