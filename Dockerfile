# 使用官方 PHP 8.1 Apache 镜像
FROM php:8.1-apache

# 设置时区变量 (默认上海，可在 docker-compose 中覆盖)
ENV TZ=Asia/Shanghai

# 1. 安装系统依赖和 Cron
# 新增: libsqlite3-dev (解决 pdo_sqlite 编译错误)
RUN apt-get update && apt-get install -y \
    git \
    unzip \
    cron \
    libzip-dev \
    libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*

# 2. 安装 PHP 扩展 (pdo_sqlite 是必须的)
RUN docker-php-ext-install pdo_sqlite zip

# 3. 启用 Apache Rewrite 模块 (虽然本项目暂未深度依赖，但作为 Web 基础很常用)
RUN a2enmod rewrite

# 4. 安装 Composer
COPY --from=composer:latest /usr/bin/composer /usr/bin/composer

# 5. 设置工作目录
WORKDIR /var/www/html

# 6. 复制项目文件
COPY . .

# 7. 安装 PHP 依赖 (生产模式)
RUN composer install --no-dev --optimize-autoloader

# 8. 配置 Crontab
# 创建定时任务文件，指定以 www-data 用户身份运行 monitor.php
# 输出重定向到 Docker 日志 (通过 /proc/1/fd/1)
RUN echo "* * * * * www-data /usr/local/bin/php /var/www/html/monitor.php > /proc/1/fd/1 2>&1" > /etc/cron.d/cdt-monitor
# 赋予权限并应用
RUN chmod 0644 /etc/cron.d/cdt-monitor && crontab /etc/cron.d/cdt-monitor

# 9. 准备启动脚本
COPY .docker/entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# 10. 暴露端口
EXPOSE 80

# 11. 设置入口点
ENTRYPOINT ["docker-entrypoint.sh"]