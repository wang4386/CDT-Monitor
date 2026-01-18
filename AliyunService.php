<?php

use AlibabaCloud\Client\AlibabaCloud;
// 移除这里的 Exception 引用，让异常直接向上抛出给 Controller 处理

class AliyunService
{
    /**
     * 获取 CDT 流量
     * @throws \Exception|ClientException|ServerException
     */
    public function getTraffic($key, $secret)
    {
        // 配置客户端
        AlibabaCloud::accessKeyClient($key, $secret)->regionId('cn-hongkong')->asDefaultClient();
        
        // 发起 RPC 请求
        $result = AlibabaCloud::rpc()
            ->product('CDT')
            ->scheme('https')
            ->version('2021-08-13')
            ->action('ListCdtInternetTraffic')
            ->method('POST')
            ->host('cdt.aliyuncs.com')
            ->request();
            
        if (isset($result['TrafficDetails'])) {
            return array_sum(array_column($result['TrafficDetails'], 'Traffic')) / (1024 * 1024 * 1024);
        }
        
        throw new \Exception("API 响应缺少 TrafficDetails 字段");
    }

    /**
     * 获取实例状态
     * @throws \Exception|ClientException|ServerException
     */
    public function getInstanceStatus($account)
    {
        AlibabaCloud::accessKeyClient($account['access_key_id'], $account['access_key_secret'])
            ->regionId($account['region_id'])
            ->asDefaultClient();
            
        $options = ['query' => ['RegionId' => $account['region_id']]];
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
    }

    /**
     * 控制实例开关机
     * @throws \Exception|ClientException|ServerException
     */
    public function controlInstance($account, $action, $shutdownMode = 'KeepCharging')
    {
        AlibabaCloud::accessKeyClient($account['access_key_id'], $account['access_key_secret'])
            ->regionId($account['region_id'])
            ->asDefaultClient();
            
        if (empty($account['instance_id'])) {
            throw new \Exception("未配置 Instance ID");
        }
        
        $options = ['query' => ['RegionId' => $account['region_id'], 'InstanceId' => $account['instance_id']]];
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
    }
}