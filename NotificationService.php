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
        if (($this->config['enable_schedule_email'] ?? '0') !== '1') return true; // 未开启也视为“处理完毕”
        
        $title = "定时任务: " . $actionType;
        $maskedKey = substr($account['access_key_id'], 0, 7) . '***';
        $details = [
            ['label' => '账号 ID', 'value' => $maskedKey],
            ['label' => '执行动作', 'value' => $actionType, 'highlight' => true],
            ['label' => '执行时间', 'value' => date('Y-m-d H:i:s')],
            ['label' => '详情说明', 'value' => $description ?: '根据预设时间表自动执行。']
        ];
        $html = $this->renderEmailTemplate($title, "您的实例已执行{$actionType}操作", $details, 'info');
        return $this->sendMail($this->config['notify_email'] ?? '', '', "CDT通知 - {$actionType}", $html);
    }

    /**
     * 发送流量告警
     * @return bool|string
     */
    public function sendTrafficWarning($accessKeyId, $traffic, $percentage, $statusText, $threshold)
    {
        if (empty($this->config['notify_email'])) return false;
        
        $title = "流量告警 - " . $statusText;
        $details = [
            ['label' => '账号 ID', 'value' => substr($accessKeyId, 0, 7) . '***'],
            ['label' => '当前流量', 'value' => $traffic . ' GB'],
            ['label' => '使用率', 'value' => $percentage . '%', 'highlight' => true],
            ['label' => '设定阈值', 'value' => $threshold . '%'],
            ['label' => '当前状态', 'value' => $statusText]
        ];
        $html = $this->renderEmailTemplate($title, "检测到流量异常或达到阈值", $details, 'warning');
        return $this->sendMail($this->config['notify_email'] ?? '', '', 'CDT流量熔断告警', $html);
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
}