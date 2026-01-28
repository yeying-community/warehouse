# Nginx 静态资源优化建议

下面配置用于加速前端静态资源加载：开启 gzip/brotli、长缓存静态资源、避免 index.html 被强缓存。

> 注意：brotli 需 nginx 编译并启用 brotli 模块，否则请移除 brotli 相关配置。

## 示例配置

```nginx
# http 或 server 块均可（推荐 http 块全局）

# 压缩
gzip on;
gzip_comp_level 6;
gzip_min_length 1k;
gzip_types text/plain text/css application/javascript application/json image/svg+xml font/woff2;
gzip_vary on;

# brotli（可选）
brotli on;
brotli_comp_level 5;
brotli_types text/plain text/css application/javascript application/json image/svg+xml font/woff2;

# 静态资源长缓存（文件名带 hash）
location ~* \.(js|css|woff2|png|svg|ico)$ {
  expires 1y;
  add_header Cache-Control "public,max-age=31536000,immutable";
}

# SPA 入口不要强缓存
location / {
  try_files $uri $uri/ /index.html;
  add_header Cache-Control "no-cache";
}
```

## 验证方法

1. 打开浏览器 Network，检查 JS/CSS 的响应头是否包含 `Content-Encoding: gzip` 或 `br`。
2. 观察 `index.html` 是否为 `no-cache`，而带 hash 的静态资源是否为 `immutable`。
3. 进入浏览器缓存后刷新，观察静态资源是否走 304 或 disk cache。
