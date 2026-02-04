
# 搭建本地环境

## 启动PG数据库(如果没有PG数据库)

1. 下载代码库(deployer)[https://github.com/yeying-community/deployer]
2. 切换到`middleware/postgresql`目录下，参考`README.md`启动数据库

## 本地配置

```shell
# 基于模板创建配置文件
cp config.yaml.template config.yaml
# 修改config.yaml中的数据库配置
```

## 初始化数据库

```shell
make build && make migrate-up
```

## 本地启动

```shell
# 创建根目录，配置文件默认为test_data
mkdir test_data
go run cmd/server/main.go -c config.yaml

# 或者使用二级制文件启动
make
build/webdav -c config.yaml

```

## 健康检查

```shell
curl http://127.0.0.1:6065/api/v1/public/health/heartbeat
```

## API 文档

- WebDAV 文件 CRUD 与认证流程：`docs/webdav-api.md`
- 认证接口统一使用 `/api/v1/public/auth/*`

## 设计文档

- 中文：`docs/zh/README.md`
- English: `docs/en/README.md`

# UCAN 认证

在 `config.yaml` 中启用 UCAN 后，可使用 `Authorization: Bearer <UCAN>` 访问需要鉴权的 API/WebDAV 资源。

```yaml
web3:
  jwt_secret: "your-super-secret-jwt-key-at-least-32-characters-long"
  token_expiration: 24h
  refresh_token_expiration: 720h
  ucan:
    enabled: true
    audience: "did:web:localhost:6065"
    required_resource: "profile"
    required_action: "read"
```

# 常用命令行操作

```shell
# 1. 安装xq，用于格式化结果
# macOS
brew install libxml2
# Ubuntu/Debian
sudo apt-get install libxml2-utils

# 2. 列出目录（PROPFIND）
curl -s -X PROPFIND -u alice:password123  -H "Depth: 1"  http://127.0.0.1:6065/ | xq .

# 3. 上传文件（PUT）
echo "Test content" | curl -X PUT -u alice:password123 --data-binary @-  http://127.0.0.1:6065/upload.txt

# 4. 下载文件（GET）
curl -u alice:password123 http://127.0.0.1:6065/upload.txt

# 5. 删除文件（DELETE）
curl -X DELETE -u alice:password123 http://127.0.0.1:6065/upload.txt

# 6. 创建目录（MKCOL）
curl -X MKCOL -u alice:password123 http://127.0.0.1:6065/new

# 7. 测试错误的密码
curl -u alice:wrongpassword http://127.0.0.1:6065/

# 8. 查询quota使用情况
curl -u alice:password123 -s http://localhost:6065/api/v1/public/webdav/quota | jq .
```

# 常用的客户端操作

```text
MACOS
打开访达 -> 选择前往菜单 -> 连接服务器 -> 输入连接地址 -> 输入用户名和密码
```

# 用户相关的操作

```shell
# 查看所有用户
./build/user -config config.yaml -action list

# 添加用户
./build/user -config config.yaml -action add \
  -username alice \
  -password secret123 \
  -directory alice \
  -permissions CRUD \
  -quota 5368709120

# 添加web3用户
./build/user -config config.yaml -action add \
  -username bob \
  -wallet 0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb \
  -directory bob \
  -permissions CRUD

# 删除用户
./build/user -config config.yaml -action delete \
  -username alice

# 更新用户
./build/user -config config.yaml -action update \
  -username alice \
  -permissions RU \
  -quota 10737418240

# 重置密码
./build/user -config config.yaml -action reset-password \
  -username alice \
  -password newsecret
```
