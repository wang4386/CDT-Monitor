# CDT-Monitor 🌩️

> **阿里云 CDT 流量监控与自动化熔断解决方案**
> 
> 旨在通过集成流量追踪、阈值熔断机制及抢占式实例保活策略，优化云端资源成本管理。

## 📖 项目概述 (Introduction)

**CDT-Monitor** 是一款专为阿里云云数据传输（CDT）用户研发的轻量级流量监控与管理系统。该系统基于 **PHP** 与 **SQLite** 架构设计，无需复杂的数据库配置即可快速部署。

其核心价值在于自动化解决云资源管理中的“黑天鹅”事件：**通过实时流量熔断机制防止 CDT 免费额度超支扣费，利用智能保活算法解决抢占式实例非预期释放导致的业务中断，并提供精准的定时任务调度，是个人开发者与中小团队控制云成本的理想管家。**

## 📸 界面预览 (Interface Preview)

| 登陆界面 | 初始化 |
| --- | --- |
| ![https://assets.qninq.cn/qning/tzONAKQq.webp](https://assets.qninq.cn/qning/tzONAKQq.webp) | ![https://assets.qninq.cn/qning/xDFaQTTX.webp](https://assets.qninq.cn/qning/xDFaQTTX.webp) |

| 后台页面 | 后台页面 |
| --- | --- |
| ![https://assets.qninq.cn/qning/WQG9I8qC.webp](https://assets.qninq.cn/qning/WQG9I8qC.webp) | ![https://assets.qninq.cn/qning/ipOStKeK.webp](https://assets.qninq.cn/qning/ipOStKeK.webp) |

| 图表展示 | 邮件展示 |
| --- | --- |
| ![https://assets.qninq.cn/qning/a5zqmuJQ.webp](https://assets.qninq.cn/qning/a5zqmuJQ.webp) | ![https://assets.qninq.cn/qning/6KEjK7bw.webp](https://assets.qninq.cn/qning/6KEjK7bw.webp) |
## ✨ 核心功能 (Core Features)

* **便捷部署架构** ：基于 SQLite 构建，无需配置 MySQL 或 Redis 等外部数据库，支持数据私有化存储与快速部署。
* **多账户聚合监控** ：提供统一管理面板，实现对多个阿里云账户下 CDT 流量使用情况及实例运行状态的集中监控。
* **流量熔断机制** ：
  * **阈值控制** ：支持自定义流量使用阈值（例如 95%）。
  * **自动化停机** ：当流量超出预设阈值时自动触发实例停机，支持配置 **普通停机** 或 **节省停机（停止计费）** 模式。
  * **告警通知** ：集成 SMTP 协议，支持通过邮件发送即时告警通知。
* **实例保活策略** ：内置保活逻辑算法，旨在防止抢占式实例在预定运行时间段内被异常回收。
* **定时任务管理** ：支持针对特定实例设定每日自动开关机计划。
* **现代化用户界面** ：采用原生 Tailwind CSS 构建，提供统一的响应式布局，确保移动端与桌面端的兼容性与视觉一致性。
* **安全保障体系** ：内置初始化配置向导，确保敏感配置信息的本地加密存储与安全性。

## 🛠️ 环境要求 (System Requirements)

* **PHP** 版本 >= 8.0
* **Composer** 依赖管理工具
* **PHP 扩展** ：`pdo_sqlite`, `curl`, `json`
* **Web 服务器** ：Nginx / Apache / OpenLiteSpeed

## 🚀 安装指南 (Installation Guide)

### 1. 获取源代码

```
git clone [https://github.com/wang4386/CDT-Monitor.git](https://github.com/wang4386/CDT-Monitor.git)
cd CDT-Monitor
```

### 2. 安装依赖包

```
composer install --no-dev
```

### 3. 配置目录权限

请确保 Web 服务器用户对项目根目录及 `data.sqlite` 文件拥有 **写入权限** 。

```
# 示例：假设 Web 根目录为 /www/wwwroot/yoursite.com
chown -R www:www /www/wwwroot/yoursite.com
chmod -R 755 /www/wwwroot/yoursite.com
```

> **🛡️ Nginx 安全配置（重要）**
> 
> 为防止数据库文件被下载，请务必在 Nginx 配置文件（server 段）中添加以下伪静态规则以禁止访问 `/data/` 目录：
> 
> ```
> location ^~ /data/ {
>     deny all;
>     return 403;
> }
> ```

### 4. 系统初始化

通过浏览器访问站点（例如 `https://monitor.yourdomain.com`）。首次访问将自动进入  **初始化向导** ，请按照提示完成管理员密码及相关参数的配置。

## 🐳 Docker 部署 (Docker Deployment)

无需配置 PHP 环境，直接使用 Docker 即可快速部署。Docker 版本已内置定时任务，**无需** 额外配置 Crontab。

### 方式一：Docker Compose (推荐)

创建一个 `docker-compose.yml` 文件：

```
services:
  cdt-monitor:
    image: qninq/cdt-monitor:latest
    # build: .
    container_name: cdt-monitor
    restart: always
    ports:
      - "43210:80"  # 将容器的 80 端口映射到宿主机的 43210 端口
    volumes:
      # 持久化数据目录，确保重启后配置和数据库不丢失
      - ./data:/var/www/html/data
      # 可选：如果你想查看 Docker 内部的日志
      # - ./logs:/var/log/nginx
    environment:
      - TZ=Asia/Shanghai  # 设置时区，对 Cron 任务很重要
```

启动服务：

```
docker-compose up -d
```

### 方式二：Docker CLI

```
docker run -d \
    --name cdt-monitor \
    --restart always \
    -p 43210:80 \
    -v $(pwd)/data:/var/www/html/data \
    -e TZ=Asia/Shanghai \
    qninq/cdt-monitor:latest
```

部署完成后，访问 `http://服务器IP:43210` 即可开始使用。

## ⚙️ 自动化任务配置 (Automation Setup)

> **注意：** 如果您使用 Docker 部署，请跳过此步骤（容器已内置自动任务）。

CDT-Monitor 依赖定时任务以实现精确的流量监控与实例保活功能。建议执行频率为 **每分钟一次** 。

您可以根据服务器环境选择以下任意一种方式：

### 方式一：命令行 Crontab (推荐)

最稳定且高效的方式，适合有服务器 SSH 权限的用户。

```
# CDT-Monitor 核心任务
* * * * * /usr/bin/php /path/to/your/project/monitor.php > /dev/null 2>&1
```

> **注意** ：请将路径替换为实际的项目部署路径，并确保使用 PHP CLI 版本执行。

### 方式二：URL 监控 (Web Cron)

如果您使用虚拟主机或希望通过第三方监控服务（如 UptimeRobot, 宝塔计划任务-访问URL）来触发，可以使用此方式。

**监控地址格式：**

```
https://您的域名/monitor.php?key=您的管理员密码
```

> **注意** ：
> 
> 1. 为了安全起见，Web 访问必须携带 `key` 参数，该参数值即为您初始化时设置的 **管理员密码** 。
> 2. 如果密码错误，系统将返回 403 禁止访问。

无论使用哪种方式，`monitor.php` 均内置了智能缓存机制，高频执行不会导致 API 调用额度的过度消耗。

## 💡 功能详解 (Advanced Documentation)

### 停机模式说明

* **普通停机 (KeepCharging)** ：停止实例后保留计算资源与 IP 地址，持续产生费用，具备快速启动特性。
* **节省停机 (StopCharging)** ：释放计算资源并停止计费。**注意：固定公网 IP 地址可能会在重启后发生变更，但弹性公网 IP (EIP) 地址保持不变。建议用于成本控制场景。**

### 抢占式实例保活机制

该机制专为**抢占式实例**设计。系统激活后，将实时监控处于“预定运行时间段”内的实例状态。若检测到非预期的关机状态（如被云平台释放），系统将尝试自动重启实例以维持服务可用性。

* *注：为防止状态频繁震荡，触发保活操作后将执行 30 分钟的冷却期。*

## 🤝 参与贡献 (Contributing)

欢迎提交 Issue 或 Pull Request 以协助改进本项目。

## 📄 开源协议 (License)

本项目遵循 [MIT License](https://www.google.com/search?q=LICENSE "null") 开源协议。

<p align="center">Created by <a href="https://qninq.cn/">青柠·倾城于你</a></p>