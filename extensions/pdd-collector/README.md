# 拼多多浏览器采集器（原型）

Manifest V3 扩展（当前版本 0.4.0），支持 `mobile.pinduoduo.com` 与 `mobile.yangkeduo.com`，从当前拼多多商品页的 `window.rawData.store` 提取商品与 SKU 白名单数据。相同 `goods_id` 可重复采集更新；服务端按 `goods_id + sku_id` 保持 SKU 身份，新增当前出现的 SKU，并保留本次页面未返回的旧 SKU 供人工核对。采集时会保存当前标签页的原始完整商品链接；页面返回的 1000 封顶库存标记为非精确，不自动覆盖素材库存。

## 安装

1. Chrome/Edge 打开扩展管理页并启用“开发者模式”。
2. 选择“加载已解压的扩展程序”。
3. 选择本目录 `extensions/pdd-collector`。
4. 打开一个已经完整加载的拼多多商品页。
5. 点击扩展图标，再点击“采集当前页面”。

采集结果可以先预览或复制 JSON。服务端启用采集 API 后，在设置页填写服务端地址和设备 Token，即可调用：

```text
POST /api/pdd-collector/products
Authorization: Bearer <device-token>
```

## 数据边界

- 只读取商品、SKU、规格、价格、库存和图片字段。
- 商品与评论采集不读取 Cookie、Authorization、完整 Headers、地址或支付信息。账号配置功能只在用户主动点击时读取当前站点的 Cookie 和默认地址 ID，保存在扩展本地并供用户复制。
- 扩展不直接访问 PostgreSQL；所有写入必须经过项目服务端。
- 当前版本使用 `<all_urls>` 以支持可配置的自建服务端地址。正式发布前应改为安装时请求具体服务端域名权限。

## 当前限制

- 当前为手动采集模式，绑定设备后可直接首次上传或再次采集更新。
- 两个拼多多域名的 Cookie 严格分开采集；切换站点后必须从对应站点地址页重新读取账号配置。
- `window.rawData.store` 内必须已经包含完整 SKU 数据；扩展会在有限深度内自动查找新版页面的嵌套 SKU、标题和图片字段。
- 页面若先把超出 JavaScript 安全整数范围的 ID 解析成 `number`，转字符串无法恢复已丢失的精度；接入真实样本时必须验证 ID，并在必要时改为从原始响应文本提取。
