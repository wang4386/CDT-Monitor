#!/bin/bash
set -e

# 确保数据目录存在
mkdir -p /var/www/html/data

# 权限修复：确保 www-data 用户可以读写数据目录
# 这对于 SQLite 和 .htaccess 生成至关重要
echo "Setting permissions for /var/www/html/data..."
chown -R www-data:www-data /var/www/html/data
chmod -R 775 /var/www/html/data

# 启动 Cron 服务
echo "Starting Cron service..."
service cron start

# 启动 Apache (在前台运行)
echo "Starting Apache..."
exec apache2-foreground