<?php

use AlibabaCloud\Client\AlibabaCloud;
use AlibabaCloud\Client\Exception\ClientException;
use AlibabaCloud\Client\Exception\ServerException;

class AliyunService
{
    /**
     * 智能重试执行器
     * 自动处理网络抖动、超时和服务端临时错误
     * * @param callable $func 业务逻辑闭包
     * @param string $action 操作名称
     * @param int $maxRetries 最大重试次数
     * @return mixed
     * @throws \Exception
     */
    private function executeWithRetry(callable $func, $action, $maxRetries = 3) // 优化点1: 将默认重试次数回调为 3 次，平衡前端等待体验
    {
        $attempt = 0;
        $lastException = null;

        while ($attempt < $maxRetries) {
            try {
                return $func();
            } catch (ClientException $e) {
                // 客户端错误(4xx)通常不重试，除非是流控限制(Throttling)
                $errorCode = $e->getErrorCode();
                if (stripos($errorCode, 'Throttling') !== false) {
                    $lastException = $e;
                    // 流控触发时，等待时间稍长
                    $this->backoff($attempt, true);
                    $attempt++;
                    continue;
                }
                throw $e; // 其他 4xx 错误直接抛出（如 AccessKey 错误）
            } catch (ServerException $e) {
                // 服务端错误(5xx)需要重试
                $lastException = $e;
            } catch (\Exception $e) {
                // 网络/cURL错误(超时、无法解析DNS等)需要重试
                $lastException = $e;
            }

            $attempt++;
            if ($attempt < $maxRetries) {
                // 记录简短日志到标准输出（可选，方便调试 Docker logs）
                // echo "Warning: Retrying $action (Attempt $attempt/$maxRetries)...\n";
                $this->backoff($attempt);
            }
        }

        throw $lastException;
    }

    /**
     * 指数退避策略
     * @param int $attempt 当前尝试次数
     * @param bool $isThrottling 是否因为流控
     */
    private function backoff($attempt, $isThrottling = false)
    {
        // 优化点2: 基础等待时间从 0.5s 提升至 1s
        // 序列变为: 1s, 2s, 4s... 3次重试总耗时控制在合理范围内
        $base = 1000000 * pow(2, $attempt); 
        if ($isThrottling) {
            $base *= 2; // 流控时等待时间翻倍
        }
        // 增加随机抖动，避免多线程/多容器并发请求撞车
        $jitter = rand(0, 500000); 
        usleep($base + $jitter);
    }

    /**
     * 获取 CDT 流量
     * @throws \Exception
     */
    public function getTraffic($key, $secret)
    {
        return $this->executeWithRetry(function () use ($key, $secret) {
            AlibabaCloud::accessKeyClient($key, $secret)
                ->regionId('cn-hongkong')
                ->asDefaultClient();
            
            $result = AlibabaCloud::rpc()
                ->product('CDT')
                ->scheme('https')
                ->version('2021-08-13')
                ->action('ListCdtInternetTraffic')
                ->method('POST')
                ->host('cdt.aliyuncs.com')
                ->options([
                    // 优化点3: 按要求缩短超时时间，提升响应速度
                    'connect_timeout' => 5.0, // 连接超时 10s -> 5s
                    'timeout' => 10.0         // 读取超时 30s -> 10s
                ])
                ->request();
            
            if (isset($result['TrafficDetails'])) {
                return array_sum(array_column($result['TrafficDetails'], 'Traffic')) / (1024 * 1024 * 1024);
            }
            
            throw new \Exception("API 响应缺少 TrafficDetails 字段");
        }, 'getTraffic');
    }

    /**
     * 获取实例状态
     * @throws \Exception
     */
    public function getInstanceStatus($account)
    {
        return $this->executeWithRetry(function () use ($account) {
            AlibabaCloud::accessKeyClient($account['access_key_id'], $account['access_key_secret'])
                ->regionId($account['region_id'])
                ->asDefaultClient();
            
            $options = [
                'query' => ['RegionId' => $account['region_id']],
                // 优化点3: 同样缩短实例状态查询的超时
                'connect_timeout' => 5.0,
                'timeout' => 10.0
            ];
            
            if (!empty($account['instance_id'])) {
                $options['query']['InstanceId'] = $account['instance_id'];
            }
            
            $result = AlibabaCloud::rpc()
                ->product('Ecs')
                ->scheme('https')
                ->version('2014-05-26')
                ->action('DescribeInstanceStatus')
                ->method('POST')
                ->host("ecs.{$account['region_id']}.aliyuncs.com")
                ->options($options)
                ->request();
            
            if (isset($result['InstanceStatuses']['InstanceStatus'][0]['Status'])) {
                return $result['InstanceStatuses']['InstanceStatus'][0]['Status'];
            }
            
            throw new \Exception("API 响应未找到实例状态 (请检查 Instance ID)");
        }, 'getInstanceStatus');
    }

    /**
     * 控制实例开关机
     * @throws \Exception
     */
    public function controlInstance($account, $action, $shutdownMode = 'KeepCharging')
    {
        return $this->executeWithRetry(function () use ($account, $action, $shutdownMode) {
            AlibabaCloud::accessKeyClient($account['access_key_id'], $account['access_key_secret'])
                ->regionId($account['region_id'])
                ->asDefaultClient();
            
            if (empty($account['instance_id'])) {
                throw new \Exception("未配置 Instance ID");
            }
            
            $options = [
                'query' => [
                    'RegionId' => $account['region_id'], 
                    'InstanceId' => $account['instance_id']
                ],
                // 优化点4: 控制操作保持一致，确保用户操作不卡死
                'connect_timeout' => 5.0, 
                'timeout' => 10.0
            ];
            
            if ($action === 'stop') {
                $options['query']['StoppedMode'] = $shutdownMode;
            }
            
            AlibabaCloud::rpc()
                ->product('Ecs')
                ->scheme('https')
                ->version('2014-05-26')
                ->action($action === 'stop' ? 'StopInstance' : 'StartInstance')
                ->method('POST')
                ->host("ecs.{$account['region_id']}.aliyuncs.com")
                ->options($options)
                ->request();
            
            return true;
        }, 'controlInstance');
    }
}