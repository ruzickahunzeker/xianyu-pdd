# 拼多多浏览器采集器（原型）

Manifest V3 扩展，从当前拼多多商品页的 `window.rawData.store` 提取商品与 SKU 白名单数据。

## 安装

1. Chrome/Edge 打开扩展管理页并启用“开发者模式”。
2. 选择“加载已解压的扩展程序”。
3. 选择本目录 `extensions/pdd-collector`。
4. 打开一个已经完整加载的拼多多商品页。
5. 点击扩展图标，再点击“采集当前页面”。

采集结果可以先预览或复制 JSON。服务端实现采集任务 API 后，在设置页填写服务端地址、设备 Token、任务 ID 和租约 Token，即可调用：

```text
POST /api/pdd-collector/tasks/{taskId}/complete
Authorization: Bearer <device-token>
```

## 数据边界

- 只读取商品、SKU、规格、价格、库存和图片字段。
- 不读取或上传 Cookie、Authorization、完整 Headers、地址或支付信息。
- 扩展不直接访问 PostgreSQL；所有写入必须经过项目服务端。
- 当前版本使用 `<all_urls>` 以支持可配置的自建服务端地址。正式发布前应改为安装时请求具体服务端域名权限。

## 当前限制

- 当前为手动采集原型，尚未自动领取服务端任务。
- `window.rawData` 必须已经包含完整 `store.skus`。
- 页面若先把超出 JavaScript 安全整数范围的 ID 解析成 `number`，转字符串无法恢复已丢失的精度；接入真实样本时必须验证 ID，并在必要时改为从原始响应文本提取。
