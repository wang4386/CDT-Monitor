# 第一阶段：构建依赖 (Builder Stage)
# 使用官方 Composer 镜像安装 PHP 依赖，避免将 Composer 及其缓存带入最终镜像
FROM composer:2 AS builder

WORKDIR /app

# 复制依赖定义文件
COPY composer.json composer.lock ./

# 安装依赖 (排除开发依赖，优化自动加载)
RUN composer install --no-dev --optimize-autoloader --ignore-platform-reqs --no-interaction --no-scripts

# 复制其余项目文件
COPY . .

# -----------------------------------------------------------------------------

# 第二阶段：运行环境 (Final Stage)
# 基于 Alpine 的 PHP-FPM 镜像，体积非常小
FROM php:8.2-fpm-alpine

# 设置镜像元数据
LABEL maintainer="CDT-Monitor-Docker"

# 1. 安装运行时系统依赖 (运行 Nginx, SQLite, Cron 必需)
# 2. 安装构建依赖 (编译 PHP 扩展必需，编译后会删除)
RUN apk add --no-cache \
    nginx \
    dcron \
    sqlite-libs \
    libcurl \
    libxml2 \
    tzdata \
    && apk add --no-cache --virtual .build-deps \
    $PHPIZE_DEPS \
    curl-dev \
    libxml2-dev \
    sqlite-dev \
    oniguruma-dev \
    \
    # 3. 安装 PHP 扩展
    && docker-php-ext-install \
    curl \
    pdo_sqlite \
    bcmath \
    simplexml \
    xml \
    mbstring \
    opcache \
    \
    # 4. 清理构建依赖和缓存，极大减小镜像体积
    && apk del .build-deps \
    && rm -rf /var/cache/apk/*

# 配置 PHP 推荐设置 (生产环境)
RUN mv "$PHP_INI_DIR/php.ini-production" "$PHP_INI_DIR/php.ini"

# 配置工作目录
WORKDIR /var/www/html

# 从 Builder 阶段复制项目文件 (包含 vendor)
COPY --from=builder /app /var/www/html

# 创建数据目录并修正权限
RUN mkdir -p /var/www/html/data && \
    chown -R www-data:www-data /var/www/html

# 配置 Nginx
COPY docker/nginx.conf /etc/nginx/http.d/default.conf

# 配置 Cron 任务 (每分钟执行一次 monitor.php)
# 将任务写入 www-data 用户的 crontab
RUN echo "* * * * * /usr/local/bin/php /var/www/html/monitor.php >> /dev/null 2>&1" >> /etc/crontabs/www-data

# 复制并配置启动脚本
COPY docker/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# 暴露端口
EXPOSE 80

# 设置容器启动入口
ENTRYPOINT ["/entrypoint.sh"]