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

# 设置环境变量
ENV TZ=Asia/Shanghai

# 安装系统依赖、编译 PHP 扩展、清理依赖、配置时区
# 将所有 RUN 指令合并以减少镜像层数
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
    && docker-php-ext-install \
    curl \
    pdo_sqlite \
    bcmath \
    simplexml \
    xml \
    mbstring \
    opcache \
    # 配置系统时区
    && cp /usr/share/zoneinfo/$TZ /etc/localtime \
    && echo $TZ > /etc/timezone \
    # 配置 PHP
    && mv "$PHP_INI_DIR/php.ini-production" "$PHP_INI_DIR/php.ini" \
    && sed -i "s/;date.timezone =/date.timezone = Asia\/Shanghai/g" "$PHP_INI_DIR/php.ini" \
    # 清理构建依赖和缓存
    && apk del .build-deps \
    && rm -rf /var/cache/apk/* \
    # 预创建目录并修正权限
    && mkdir -p /var/www/html/data \
    && chown -R www-data:www-data /var/www/html \
    # 配置 Cron (每分钟执行)
    && echo "* * * * * /usr/local/bin/php /var/www/html/monitor.php >> /dev/null 2>&1" >> /etc/crontabs/www-data

# 配置工作目录
WORKDIR /var/www/html

# 复制 Nginx 配置 (利用缓存，变更频率低)
COPY docker/nginx.conf /etc/nginx/http.d/default.conf

# 复制并配置启动脚本
COPY docker/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# 最后复制项目代码 (变更频率高，放在最后)
COPY --from=builder --chown=www-data:www-data /app /var/www/html

# 暴露端口
EXPOSE 80

# 设置容器启动入口
ENTRYPOINT ["/entrypoint.sh"]