# Docker 拼多多下单脚本规划

## 0. 当前实施结论

当前需要完成的核心是 **Docker Chromium 创建拼多多待付款订单**。闲鱼实物物流发货作为后续独立模块，新建“发货工作台”，不把发货表单和批量操作整合进现有“订单管理”。订单管理只保留订单与履约状态查看。

完整业务链路：

```text
闲鱼订单同步
  → 订单管理中进入待采购
  → 原子领取下单任务
  → 记录拼多多待付款基线
  → 通过闲鱼 order_id 修改拼多多默认收货地址
  → Chromium 打开指定 goods_id + sku_id 结算页
  → 核对商品、SKU、数量、地址和最低 0.5 元利润
  → 点击“立即支付”创建待付款订单
  → 从待付款列表唯一确认 order_sn
  → 回填 pdd_order_id，pdd_ordered 仍为 false
  → 人工核对并付款
  → 确认拼多多已付款，pdd_ordered=true
  → 同步拼多多物流
  → 进入独立“发货工作台”
  → 闲鱼实物物流接口成功
  → xianyu_shipped=true
```

## 1. 目标与边界

首期只支持单个拼多多账号，但任务、浏览器数据目录、账号锁和审计记录均保留 `pdd_account_id`，后续可扩展到多账号。

首期目标：

1. 从履约 API 领取一笔已完成 SKU 映射、未下单的闲鱼订单。
2. 调用已实现的地址修改接口，把拼多多默认地址改为当前闲鱼订单的收货信息；手机号是否临时变更由后续开关决定。
3. 在 Docker 内启动 Chromium，直接打开 `order_checkout.html?sku_id={source_sku_id}&goods_id={source_goods_id}`。
4. 设置购买数量，校验收货地址、SKU、数量和金额后，点击“立即支付”创建待付款订单。
5. 打开待付款列表，从页面原始数据识别刚创建的 `order_sn`，回填履约记录。
6. 保留人工审核付款；付款后进入等待拼多多发货阶段。
7. 取得拼多多快递公司和运单号后，调用闲鱼实物物流发货接口，成功后更新履约状态。

首期不做：自动付款、绕过验证码或风控、多账号调度、自动处理映射冲突、修改现有闲鱼滑块验证逻辑。

## 2. 推荐架构

新增独立的 `pdd-worker` 容器，不把拼多多点击脚本塞入主服务进程。

```text
定时调度
   ↓
pdd-worker ──Bearer API──> xianyu app ──> 履约数据库
   │                              │
   └──Chromium──> 拼多多          └──地址修改 HTTP 请求──> 拼多多
```

职责分工：

- 主服务：订单和 SKU 映射、拼多多账号配置、地址修改、幂等、状态机、审计和密钥管理。
- `pdd-worker`：页面导航、SKU 选择、数量填写、下单页校验、截图和浏览器错误采集。
- 人工：首期处理登录过期、验证码、风控、最终金额核对与付款。

## 3. 状态流程

```text
pending
  → validating
  → address_applied
  → browser_preparing
  → submitting_unpaid_order
  → unpaid_order_created
  → awaiting_manual_payment
  → ordered
  → waiting_shipment
  → pdd_shipped
  → xianyu_shipping
  → xianyu_shipped
```

异常分支：

- `blocked_mapping`：`mapping_status != mapped` 或缺少 `goods_id/sku_id`。
- `blocked_login`：拼多多 Cookie 或持久化会话已失效。
- `blocked_captcha`：出现验证码或风控，停止自动点击。
- `failed_address`：地址修改失败，未进入结算页。
- `blocked_address`：结算页或创单后订单详情的收货人、省市区、详细地址与本次闲鱼订单快照不匹配。
- `failed_browser`：选规格、数量、页面校验或导航失败。
- `blocked_profit`：拼多多实付金额不满足最低 0.5 元利润。
- `blocked_logistics`：快递编码、运单号或卖家发货地址 ID 不完整。
- `failed_xianyu_ship`：闲鱼实物物流发货请求失败，不得把本地订单标记为已发货。
- `result_unknown`：结果无法确认，禁止自动重试。

每一步均记录 `order_id`、`pdd_account_id`、`attempt`、开始/结束时间、错误类型和截图路径。

所有 `blocked_*`、`result_unknown` 和订单候选不唯一状态同时写入统一异常事件表。首期先在管理页展示未处理报警数和差异详情；邮件、Webhook 或其他通知渠道后续再定，异常事件先保留可扩展的通知状态。

## 4. API 补齐规划

现有 API 继续复用：

- `GET /api/fulfillment/orders?pdd_ordered=false&mapping_status=mapped`
- `POST /api/fulfillment/orders/{order_id}/pdd-address/apply`
- `PUT /api/fulfillment/orders/{order_id}`
- `DELETE /api/fulfillment/orders/{order_id}/pdd-address/lock`

实现 worker 前建议增加：

1. `POST /api/fulfillment/orders/{order_id}/claim`：原子领取任务，返回有限时租约，防止多 worker 重复执行。
2. `POST /api/fulfillment/orders/{order_id}/heartbeat`：续租并上报当前步骤。
3. `POST /api/fulfillment/orders/{order_id}/browser-result`：回传浏览器结果、实际 SKU、数量、金额、页面订单号和错误类型。
4. `POST /api/fulfillment/orders/{order_id}/abort`：人工放弃时结束任务并安全释放账号锁。

独立发货工作台和外部脚本共用以下规划接口：

1. `GET /api/shipping/orders`：按发货状态、闲鱼账号、订单号和运单号查询发货任务。
2. `POST /api/shipping/orders/{order_id}/sync-logistics`：同步或接收该订单的拼多多物流。
3. `POST /api/shipping/orders/{order_id}/preview`：校验快递映射、运单号、卖家地址和闲鱼订单状态，不对外发送。
4. `POST /api/shipping/orders/{order_id}/ship`：使用 `Idempotency-Key` 执行闲鱼实物发货，供页面和 curl 共用。
5. `GET /api/shipping/operations/{operation_id}`：查询发货操作结果，包括 `processing/shipped/failed/result_unknown`。

curl 发货不直接把 Cookie 传给脚本，仍使用履约 Bearer 密钥：

```bash
# 先预检查
curl -X POST \
  -H "Authorization: Bearer $FULFILLMENT_API_KEY" \
  "http://127.0.0.1:59188/api/shipping/orders/${XIANYU_ORDER_ID}/preview"

# 确认后发货
curl -X POST \
  -H "Authorization: Bearer $FULFILLMENT_API_KEY" \
  -H "Idempotency-Key: ${XIANYU_ORDER_ID}-ship-v1" \
  -H "Content-Type: application/json" \
  -d '{"tracking_number":"...","cp_code":"YUNDA","address_id":"..."}' \
  "http://127.0.0.1:59188/api/shipping/orders/${XIANYU_ORDER_ID}/ship"
```

请求体可覆盖系统预填的运单号、`cpCode` 和卖家发货地址，但所有覆盖必须记录审计；不提供时使用工作台已确认数据。

`claim` 成功后应一次返回 worker 所需的非敏感任务数据：

```json
{
  "order_id": "3313845698508005453",
  "pdd_account_id": "9262fda6-6422-44fb-998a-975c89aad634",
  "source_goods_id": "986758178156",
  "source_sku_id": "1944939066689",
  "quantity": 1,
  "mapping_status": "mapped",
  "lease_token": "...",
  "lease_expires_at": 1786763000
}
```

Cookie 不随任务列表和 HTTP 接口返回。单机 Docker Compose 部署中，worker 使用现有 `DATABASE_URL` 和 `XIANYU_DATA_KEY`，按任务的 `pdd_account_id` 直接读取并解密设置中保存的账号。任务领取、续租和结果回传仍使用独立履约 Bearer 密钥，不以“Docker 内网”为理由取消认证。

## 5. Chromium 与凭证

- 使用官方 Playwright Chromium 运行时，版本在镜像中锁定，不在容器启动时下载。
- 每个 `pdd_account_id` 使用独立的 persistent context 目录，挂载到独立 volume。
- 每笔任务创建浏览器 Context 前重新读取对应账号 Cookie 和 User-Agent；Cookie 不写入日志、截图或任务响应，任务完成后关闭该 Context。
- 首期默认 headless；遇到登录或风控不进行规避，转人工处理。
- 截图仅保留失败页面和付款前核对页，设置保留期并对姓名、手机号和地址按敏感数据管理。

## 6. 完整执行流程

### 6.1 通过闲鱼订单 ID 修改地址

worker 领取的主键始终是闲鱼 `order_id`。它不直接传收货地址，而是在打开 Chromium 前调用现有接口：

```bash
curl -X POST \
  -H "Authorization: Bearer $FULFILLMENT_API_KEY" \
  -H "Idempotency-Key: ${XIANYU_ORDER_ID}-address-v${ATTEMPT}" \
  "http://app:59188/api/fulfillment/orders/${XIANYU_ORDER_ID}/pdd-address/apply"
```

主服务使用该闲鱼订单 ID 读取收货人、手机号和地址，匹配行政区，再修改当前拼多多账号的默认地址。手机号是否改为临时号由后续的账号/任务开关决定；无论开关是否开启，手机号都不作为订单自动匹配的必要条件。worker 只在返回 `status=applied` 时继续；`failed`、`result_unknown` 或锁冲突都必须停止。

浏览器进程可以提前启动以恢复会话，但不能使用修改地址前已经加载的结算页提交。地址修改成功后必须重新导航到 `order_checkout.html`，或对旧页面强制刷新并重新校验。

### 6.2 记录待付款列表基线

点击“立即支付”前，先访问：

```text
https://mobile.pinduoduo.com/orders.html?type=1
```

从页面内嵌原始数据读取现有待付款 `orderSn/order_sn`，保存为 `before_order_sns`。不依赖订单卡片的随机 class。

### 6.3 打开结算页与数量设置

直接打开：

```text
https://mobile.pinduoduo.com/order_checkout.html?sku_id={source_sku_id}&goods_id={source_goods_id}
```

这一路径已经由 `source_sku_id` 确定 SKU，无需再从商品页按文本选规格。页面打开后仍必须从页面状态或网络响应确认实际 `goods_id + sku_id`与任务一致。

数量控件优先使用稳定可访问性属性：

- 当前数量：`input[type=number][aria-label^="当前数量为"]`
- 增加数量：`[role=button][aria-label="增加数量"]`
- 减少数量：`[role=button][aria-label="减少数量"]`

首选直接填写 input 并触发 `input/change/blur`，再读回数值校验；如果页面状态不接受直接填写，才按差值点击增减按钮。每次点击后都重读 input，不根据点击次数假设结果。

### 6.4 校验并创建待付款订单

点击前必须核对：

- 页面 `goods_id + sku_id` 与任务一致；
- 页面数量与闲鱼订单数量一致；
- 收货人、省市区和标准化后的详细地址与本次闲鱼订单快照一致；
- 实付金额满足最低 0.5 元利润约束，且不超过人工设定的绝对采购上限。

默认利润校验：

```text
可用收入 = 闲鱼订单实收金额 - 已知平台费用 - 其他预留成本
预计利润 = 可用收入 - 拼多多结算页实付金额
允许创单 = 预计利润 >= 0.50 元
```

金额计算在服务端统一使用“分”的整数，禁止用浮点数比较。拼多多优惠券后价格降低可以通过；价格上涨导致利润小于 50 分时转 `blocked_profit`，不点击“立即支付”。如后续支持人工放行，必须记录操作人、放行上限和原因。

“立即支付”的 class 会变，`aria-label="[object Object]"` 也不可靠。定位策略为：

1. 在页面底部可见区域查找文本精确等于“立即支付”的可见 `span`。
2. 向上寻找最近的 `[role=button]`，确认唯一、可见且未禁用。
3. 点击后同时监听创单网络响应、URL 变化和支付界面，不以“点击未报错”当作成功。

此点击会创建待付款订单，不等于完成付款。如页面行为变成直接扣款，必须立即停用自动点击并改为人工确认。

### 6.5 从待付款列表确定拼多多订单 ID

创单后重新访问 `orders.html?type=1`，从内嵌数据中解析订单。上传的页面已确认同时存在 camelCase 和 snake_case 两套字段，解析器应归一化：

| 归一化字段 | 页面中可能的字段 | 用途 |
| --- | --- | --- |
| `pdd_order_id` | `orderSn` / `order_sn` | 最终回填的拼多多订单号 |
| `group_order_id` | `groupOrderId` / `group_order_id` | 辅助审计，不代替 `order_sn` |
| `address_id` | `addressId` / `address_id` | 只确认使用了配置的拼多多地址记录，不用于区分不同闲鱼订单 |
| `goods_id` | `orderGoods[].goodsId` / `order_goods[].goods_id` | 匹配商品 |
| `sku_id` | `orderGoods[].skuId` / `order_goods[].sku_id` | 匹配 SKU |
| `quantity` | `goodsNumber` / `goods_number` | 匹配数量 |
| `amount` | `orderAmount` / `order_amount` | 核对金额，注意两套数据可能分别为字符串元和整数分 |
| `order_time` | `orderTime` / `order_time` | 限定创单时间窗口 |
| `payment_deadline` | `nextPayTimeOut` / `next_pay_time_out` | 人工付款超时提醒 |

候选订单必须同时满足：

1. `order_sn` 不在 `before_order_sns` 中；
2. `goods_id` 等于任务 `source_goods_id`；
3. `sku_id` 等于任务 `source_sku_id`；
4. `quantity` 等于任务数量；
5. `order_time` 在本次点击创单的合理时间窗口内。

只有一个候选时，先取得 `order_sn`，再访问 `order.html?order_sn={order_sn}` 或对应订单详情接口，二次核对收货人、省市区和标准化详细地址。地址详情一致后才能自动回填 `pdd_order_id`。零个候选则轮询有限次；多个候选或地址不匹配转人工核对并创建报警，不按“最新一笔”猜测。如果创单网络响应直接返回 `order_sn`，也必须完成订单列表和订单详情两层交叉确认。

此时只回填 `pdd_order_id`，保持 `pdd_ordered=false`，因为订单仍未付款。人工确认付款成功后，再更新 `pdd_ordered=true`。如果待付款订单超时关闭，应保留原订单号审计记录，通过明确的“重新创单”操作开启新 attempt，不覆盖历史。

上传样本中的一笔数据证明该路径可用：`goods_id=986758178156`、`sku_id=1944939066689`、`goods_number=1`可对应到 `order_sn=260815-625140067923519`。

新上传样本进一步确认，待付款 HTML 的首条数据为当前最新订单：`order_sn=260815-506000873043519`、`goods_id=975995542258`、`sku_id=1930347805146`、`quantity=1`、`address_id=60914015706`、`order_time=1786775665`、`order_amount=1090`分。但“首条”只用于缩小候选，仍必须执行差集和全字段核对。

第二份 curl 还确认待付款页面会调用：

```text
POST /proxy/api/api/caterham/v3/query/my_order_group
POST /proxy/api/api/aristotle/order_list_v4
```

首期优先直接 GET `orders.html?type=1` 并解析服务端内嵌订单数据，因为已有完整样本可验证。底层接口作为后续优化和 HTML 结构变化时的备选，不盲目复制抓包中可变的请求参数。

拼多多默认地址是反复编辑的同一条记录，因此不同闲鱼订单通常拥有相同的 `address_id`。`address_id` 只是配置正确性检查，不是订单归属匹配条件。

地址标准化只允许消除无语义差异，例如首尾空格、全/半角标点、重复省市名和连续空白；不允许模糊猜测门牌号、小区名或收货人。

页面选择器不能依赖可变 CSS class，数据识别不能依赖视觉卡片顺序。应优先使用可访问性属性、稳定文案、页面原始状态与网络响应交叉校验。

### 6.6 单任务脚本生命周期与队列

每次 Chromium 执行只处理一笔闲鱼订单，不在同一页面上连续切换多笔任务：

```text
领取 1 笔任务
  → 记录待付款基线
  → 修改地址
  → 重新打开结算页
  → 核对并创建待付款订单
  → 关闭该结算页/结束本次浏览器操作
  → HTTP 获取最新待付款页面
  → 唯一核对新 order_sn
  → 回填成功或进入异常状态
  → 释放 worker 租约，但已创单订单保留付款约束
  → 领取下一笔任务
```

Chromium 进程可以长驻以复用登录会话，但每笔任务必须创建独立 page，完成后关闭 page。如出现验证码、登录失效或风控，停止整个账号队列，不继续处理下一笔。

同一拼多多账号始终为单并发。前一笔如果没有唯一核对到 `order_sn`，不允许继续下一笔，因为否则无法确定待付款列表中的新订单属于哪个闲鱼订单。只有“已唯一回填”或“明确未创单”才能继续队列。

## 7. 拼多多与闲鱼订单核对

不需要把拼多多订单所有字段写入履约主表，但必须在“下单任务审计记录”中保留本次核对快照。

| 核对项 | 闲鱼/系统期望值 | 拼多多实际值 | 不匹配处理 |
| --- | --- | --- | --- |
| 商品 | `source_goods_id` | `goods_id` | `blocked_mapping`，禁止创单 |
| SKU | `source_sku_id` | `sku_id` | `blocked_mapping`，禁止按规格文字猜测 |
| 数量 | 闲鱼 `quantity` | 结算页数量 | 允许重设一次，仍不一致则 `blocked_quantity` |
| 收货信息 | 收货人 + 省市区 + 标准化详细地址 | 结算页及创单后订单详情 | `blocked_address`，暂停账号队列并创建报警 |
| 利润 | 最低 50 分 | 闲鱼可用收入减拼多多实付 | `blocked_profit`，禁止创单 |

核对快照至少保留：期望值、实际值、差异、页面 URL、截图、任务 attempt 和时间。不匹配时不自动改映射、不自动降低利润门槛、不自动换地址。手机号及未来的临时手机号开关可写入快照供人工查看，但不参与自动匹配成功/失败的判定。

## 8. 物流与闲鱼发货

> 本章是已确认的技术资料和候选流程，不属于当前 Docker 下单脚本的实施范围。发货业务流程、页面交互和自动化边界尚未定稿，等地址列表 Response 和拼多多物流数据样本齐全后单独评审。

### 8.1 2026-08-16 拼多多待收货页面样本

已保存并核对两份实际页面源码，作为下一阶段物流解析器的基准样本：

- 待收货列表：`https://mobile.pinduoduo.com/orders.html?type=3`。列表原始数据可同时提供多笔待收货订单，不依赖随机 CSS class。
- 订单详情：`https://mobile.pinduoduo.com/order.html?order_sn={pdd_order_id}&page_from=1&main_orders=1`。`refer_page_*`、`is_tester` 等追踪参数不是读取详情的稳定主键，核心主键是 `order_sn`。

样本确认可归一化以下字段：

| 归一化字段 | 页面样本字段/值 | 用途 |
| --- | --- | --- |
| `pdd_order_id` | `order_sn`，如 `260816-024421353043519` | 与履约记录中的拼多多订单号精确匹配 |
| `pdd_order_status` | `orderStatusPrompt`，如“待收货” | 判断是否已经由商家发货 |
| `goods_id` | `goods_id` / `goodsId` | 商品交叉核对 |
| `sku_id` | 商品跳转参数中的 `sku_id` | SKU 交叉核对 |
| `tracking_number` | `trackingNumber` / `tracking_number` | 回填运单号 |
| `shipping_id` | 物流跳转参数中的 `shipping_id` | 拼多多承运商标识，仅作快递映射辅助 |
| `logistics_company` | 详情 `物流公司`，样本为“韵达快递” | 映射闲鱼 `cpCode` |
| `latest_trace` | `lastTrace` / `traces[0].info` | 展示最近物流状态与异常判断 |
| `address_id` | `addressId` / `addressUid` | 拼多多收货地址审计；不是闲鱼卖家发货地址 |

样本订单 `260816-024421353043519` 同时出现运单号 `465592976160187`、`shipping_id=121` 和物流公司“韵达快递”，说明待收货列表适合批量发现订单与运单号，订单详情适合按 `order_sn` 二次确认物流公司、运单号和最新轨迹。

明日实现建议：先解析 `orders.html?type=3` 得到候选集合，只处理已经回填 `pdd_order_id` 且尚未 `pdd_shipped` 的履约订单；按 `order_sn` 精确匹配后，必要时访问详情补全物流公司。严禁仅按列表第一条、收件人或商品标题猜测。匹配成功后更新 `pdd_shipped=true + logistics_company + tracking_number`，再进入独立发货工作台，不直接调用闲鱼发货。

参考任务 `01a003a2-1ca5-7370-96be-a73e370dc0cd` 已确认闲鱼实物物流发货接口：

```text
mtop.taobao.idle.logistics.merchant.consign.offline
```

业务参数：

```json
{
  "orderInfos": "[{\"tradeId\":\"闲鱼订单ID\"}]",
  "mailNo": "快递单号",
  "cpCode": "YUNDA",
  "addressId": 25152147714
}
```

- `tradeId`：当前履约记录的闲鱼订单 ID。
- `mailNo`：从拼多多订单同步得到的运单号。
- `cpCode`：闲鱼支持的快递公司编码，例如韵达 `YUNDA`、中通 `ZTO`、顺丰 `SF`、申通 `STO`、极兔 `HTKY`、圆通 `YTO`。
- `addressId`：当前闲鱼卖家账号的发货地址 ID，不是拼多多收货地址 ID，也不能在多账号间共用。

项目现有 MTOP 客户端已具备动态签名、Cookie 更新、token 刷新和请求重试，应新增 `ConsignOfflineContext`，不复制抓包中的一次性 `t/sign`。`orderInfos` 需按闲鱼要求编码成“JSON 数组的字符串”。

发货前置条件：

1. 闲鱼订单属于当前账号且状态为待发货。
2. `pdd_ordered=true`、`pdd_shipped=true`。
3. `tracking_number` 非空，拼多多快递公司已唯一映射到闲鱼 `cpCode`。
4. 当前闲鱼账号已配置并验证卖家发货 `addressId`。
5. 该订单没有成功发货记录，本次请求持有唯一幂等键。

调用规则：

- 只有闲鱼 MTOP 明确返回成功后，才设置 `xianyu_shipped=true`。
- token 过期可使用现有客户端刷新后有界重试；结果不确定时先同步闲鱼订单状态，不盲目重复发货。
- `cpCode` 优先由闲鱼快递公司列表接口动态拉取并缓存，名称映射无法唯一时转人工选择。
- 卖家发货 `addressId` 通过 `merchant.delivery.address.list.query` 按闲鱼账号动态获取；待补充接口 Response 样本以确认字段路径。

## 9. 独立发货工作台

> 本章为待评审设计，当前只确定“独立页面，不整合进订单管理”，暂不进入开发。

发货工作台是独立业务页面，专门处理“拼多多物流已产生 → 闲鱼完成发货”。订单管理不放发货按钮、快递编辑表单或批量发货操作，只展示“闲鱼发货状态”。

发货工作台和订单管理共享 `orders + order_fulfillments`，不复制订单主数据。发货过程、幂等结果和失败原因保存在独立发货操作记录中。

### 9.1 页面导航与分组

主导航新增“发货工作台”，默认只展示需要操作的订单。顶部分组：

1. `待同步物流`：拼多多已付款，但还没有快递信息。
2. `待发货`：已有快递公司和运单号，闲鱼尚未发货。
3. `需补充`：缺少 `cpCode`、运单号或卖家发货地址。
4. `发货失败`：闲鱼明确返回失败，可根据错误修复后重试。
5. `结果待确认`：请求结果不确定，禁止重复发货，先同步闲鱼状态。
6. `已发货`：只读历史，支持查询和查看审计。

筛选项：闲鱼账号、闲鱼订单号、拼多多订单号、商品标题、快递公司、运单号、状态和时间范围。

### 9.2 发货列表字段

每行显示：

- 闲鱼账号、闲鱼订单号、商品、规格和数量；
- 拼多多订单号和拼多多发货状态；
- 拼多多快递公司原始名称、闲鱼 `cpCode` 及映射状态；
- 运单号、拼多多发货时间、最近物流同步时间；
- 卖家发货地址的脱敏摘要和 `addressId`；
- 闲鱼发货状态、最近错误和操作时间。

详细收货地址不在发货列表默认展示，发货所需的 `addressId` 是卖家发货地址，与买家收货地址严格分离。

### 9.3 单笔发货流程

1. 点击“发货”，服务端读取最新闲鱼订单和履约记录。
2. 弹出发货预览，展示闲鱼订单、拼多多订单、商品、数量、快递公司、`cpCode`、运单号和卖家发货地址。
3. 允许人工修正快递公司映射、运单号和发货地址，修改必须记录审计。
4. 用户二次确认后，服务端重新校验订单归属、平台状态、物流信息和是否已发货。
5. 创建幂等发货操作，调用 `mtop.taobao.idle.logistics.merchant.consign.offline`。
6. 闲鱼明确返回成功后，更新 `xianyu_shipped=true`并完成操作记录。
7. 明确失败进入“发货失败”；结果不确定进入“结果待确认”。

前端提交的账号 ID、订单归属和已发货状态都不是可信边界，必须由服务端按闲鱼订单重新确认。

### 9.4 批量发货流程

批量发货必须采用“预检查—确认—逐笔执行”，不允许勾选后立即发送：

1. 用户勾选待发货订单。
2. 服务端逐笔预检查，分成“可发货”、“需补充”和“禁止发货”三组。
3. 页面展示可发货数量、跳过数量及每笔阻断原因。
4. 用户确认后，只对“可发货”订单逐笔调用闲鱼接口。
5. 每笔订单独立幂等、独立记录结果，单笔失败不影响其他订单。

首个发布版可先关闭批量提交，只上线批量预检查和单笔确认；单笔流程稳定后再开放批量发货。

### 9.5 快递公司映射

- 从闲鱼 `com.taobao.idle.logistics.new.get.companies` 拉取官方 `code + name` 列表并缓存。
- 先用已确认映射表转换拼多多快递名称，例如韵达→`YUNDA`、中通→`ZTO`。
- 名称模糊匹配只能产生建议，不能直接发货。
- 人工确认新映射后可保存为全局映射，并记录来源和最后验证时间。

### 9.6 卖家发货地址

使用已发现的地址列表接口：

```text
mtop.alibaba.idle.seller.platform.merchant.delivery.address.list.query
```

发货工作台按闲鱼账号动态获取地址列表，展示脱敏后的联系人、地区和详细地址，并允许用户选择该账号的默认发货地址。需要先用接口 Response 确认 `addressId` 的实际字段路径。

地址选择按闲鱼账号隔离，不得使用拼多多收货地址 ID，不得在多个闲鱼账号之间共用 `addressId`。已保存地址在发货前如无法从最新列表中找到，该订单转“需补充”。

### 9.7 异常与恢复

| 异常 | 系统处理 |
| --- | --- |
| 缺少运单号 | 进入“待同步物流”，不得发货 |
| 快递公司无法映射 | 进入“需补充”，人工选择 `cpCode` |
| 卖家地址缺失/失效 | 刷新地址列表后重新选择 |
| 闲鱼 Cookie/token 过期 | 使用现有 MTOP 刷新机制有界重试，仍失败则要求重新登录 |
| 闲鱼明确返回失败 | 保留 `xianyu_shipped=false`，显示平台错误 |
| 网络中断/响应不可解析 | 标记 `result_unknown`，立即同步闲鱼订单状态 |
| 闲鱼已显示发货 | 将本地状态修正为已发货，不重放发货请求 |
| 运单号被人工修改 | 记录修改前后值、操作人和原因 |

### 9.8 自动与人工边界

首期自动化：同步物流、解析快递信息、推荐 `cpCode`、读取卖家地址、发货预检查、用户确认后调用闲鱼发货接口。

首期人工：拼多多付款、不可唯一的快递映射、卖家发货地址选择、闲鱼发货前的最终确认、失败和结果不确定处理。

## 10. 分阶段实施

### P0：接口与任务状态

- 增加 claim、heartbeat、browser-result、abort 接口和数据迁移。
- 在履约任务详情中补充并校验闲鱼订单的 `quantity` 和 `amount`；当前订单表已有这两个字段，但履约列表尚未返回。
- 定义步骤状态、错误分类、租约过期和重试规则。
- 将现有地址锁与 worker 租约关联。

### P1：只读 Chromium 验证

- 构建 `pdd-worker` 镜像和 persistent volume。
- 验证 Chromium 可启动、Cookie 可恢复、结算页和待付款列表可读取。
- 实现待付款页面原始数据解析和 camelCase/snake_case 归一化，本阶段不创建订单。

### P2：下单前自动化

- 串行执行待付款基线记录、地址修改、结算页打开、数量填写和订单页校验。
- 点击“立即支付”创建待付款订单，从待付款列表唯一识别 `order_sn`。
- 实现“一个 page 只处理一笔订单”的单账号串行队列；本笔结果未确认时暂停队列。
- 使用拼多多待付款 HTML 快照实现创单前后差集和全字段核对，并为底层订单列表接口保留适配层。
- 停在人工审核付款阶段，回传订单号、核对数据和截图。
- 完成断网、重启、超时和重复任务测试。

### P3：订单回填与运维

- 创建待付款订单后回填 `pdd_order_id`，但保持 `pdd_ordered=false`。
- 人工付款后确认订单已转为待发货，再更新 `pdd_ordered=true`并释放下单锁。
- 管理页展示 worker 心跳、当前任务、截图、错误和重试/放弃操作。
- 增加健康检查、有界重试、日志脱敏、截图清理和告警。

### P4：物流同步与闲鱼发货

- **暂缓实施，先完成单独业务评审。**
- 新建独立“发货工作台”，实现待同步、待发货、需补充、发货失败、结果待确认和已发货分组。
- 实现卖家发货地址列表查询、账号级默认选择和失效校验。
- 实现快递公司列表缓存及拼多多物流名称到闲鱼 `cpCode` 的可管理映射。
- 新增 `ConsignOfflineContext` 和实物物流发货接口，加入订单归属、状态、幂等和结果核验。
- 同时提供发货页面与 Bearer 密钥 curl 调用入口，两者共用预检查、幂等、审计和状态同步逻辑。
- 先上线单笔发货和批量预检查，稳定后再开放逐笔批量提交。
- 发货成功后自动更新 `xianyu_shipped=true`；不确定结果通过订单同步确认。

## 11. 验收条件

- 同一拼多多账号不会并发处理两笔订单。
- 任何重试都不会重复修改地址或重复创建拼多多订单。
- 只有 `mapping_status=mapped` 且 `goods_id + sku_id` 完整时才能进入浏览器流程。
- 浏览器页面的 SKU、数量、金额和收货地址校验失败时禁止继续。
- 预计利润低于 50 分时禁止自动创建拼多多订单。
- 闲鱼订单、快递单号、`cpCode` 或卖家发货地址 ID 任一不可确认时禁止发货。
- 闲鱼平台未明确返回成功时，本地 `xianyu_shipped` 不得置为 `true`。
- 出现登录、验证码、风控或结果不确定时停止自动化并留下审计记录。
- 容器重启后可恢复会话和未完成任务，不会把旧页面当成新订单。
