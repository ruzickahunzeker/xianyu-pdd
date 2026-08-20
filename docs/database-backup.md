# 数据库备份与恢复

## 功能范围

系统设置中的“数据库备份”支持管理员手动创建备份、自动定时备份、查看校验结果和下载备份。每次成功备份会产生：

- 数据库完整快照：PostgreSQL 为 `pg_dump` custom archive，SQLite 为一致性数据库副本；
- `*.manifest.json`：创建时间、大小、数据库类型、SHA-256、SKU 映射行数和校验状态；
- `xianyu-sku-mappings-*.csv`：闲鱼 SKU 与拼多多 SKU 的独立可读导出。

备份默认写入 `data/backups`，Docker Compose 会通过现有 `./data:/app/data` 持久化。可用 `XIANYU_BACKUP_DIR` 修改目录。

## 密钥必须另存

数据库里的 Cookie、拼多多账号配置和其他敏感字段使用 `XIANYU_DATA_KEY` 加密。备份清单只记录密钥是否已配置，不保存密钥明文。恢复时必须使用原来的 `XIANYU_DATA_KEY`，否则数据库虽然能恢复，加密字段仍无法解密。

建议把密钥保存到与数据库备份不同的密码管理器或离线介质中。

## PostgreSQL 恢复演练

不要直接覆盖正在运行的数据库。先创建一个临时数据库验证：

```bash
createdb xianyu_restore_test
pg_restore --no-owner --no-privileges --dbname xianyu_restore_test xianyu-postgres-YYYYMMDD-HHMMSS.dump
go run ./cmd/dbverify "postgres://user:password@127.0.0.1:5432/xianyu_restore_test"
```

确认管理员、账号、商品、订单、素材库、SKU 映射和履约数据无误后，再安排停机切换。正式恢复前应先对当前数据库再做一次保护性备份。

## SQLite 恢复演练

停止应用后保留原数据库，再把下载的 `.db` 副本放到新的路径并运行：

```bash
go run ./cmd/dbverify "sqlite:///path/to/restored.db"
```

验证通过后才修改服务的数据库路径。不要在服务运行时覆盖当前 SQLite 文件。

## 保留策略

自动备份默认建议每 24 小时一次，保留 14 份。系统只会清理 `XIANYU_BACKUP_DIR` 内、由有效清单关联的旧备份、映射 CSV 和清单，不会扫描或删除其他目录。

本机备份不能代替异地备份。整条链路稳定后，建议把已验证的备份再同步到 NAS 或对象存储，并至少每月完成一次恢复演练。
