import assert from "node:assert/strict";
import test from "node:test";

import { collectMallSN, collectPDDProduct } from "./collector.js";

test("collectMallSN extracts the decoded mall_sn from pddRoute", () => {
  const mallSN = "CgI2WRIIS0xJNU5SeWUaEC2sGini4/ClafQ5Mz0xMQsiEA==";
  const store = { nested: { pddRoute: `mall_page.html?mall_sn=${encodeURIComponent(mallSN)}&decrypt_mall_sn=1` } };
  assert.deepEqual(collectMallSN(store), { value: mallSN, path: "store.nested.pddRoute.mall_sn" });
});

test("collectPDDProduct includes mall_sn and its source path", () => {
  globalThis.location = { href: "https://mobile.pinduoduo.com/goods.html?goods_id=123" };
  const result = collectPDDProduct({ store: { goodsID: 123, goodsName: "测试商品", skus: [{ skuId: 456, goodsId: 123, quantity: 1, specs: [{ spec_key: "款式", spec_value: "A" }] }], mall: { pddRoute: "mall_page.html?mall_sn=mall-token&decrypt_mall_sn=1" } } });
  assert.equal(result.goods.mall_sn, "mall-token");
  assert.equal(result.mall_sn_source_path, "store.mall.pddRoute.mall_sn");
});
