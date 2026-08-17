import assert from "node:assert/strict";
import test from "node:test";

import { collectDefaultAddressID, collectMallSN, collectPDDProduct, isPDDAddressPage } from "./collector.js";

test("isPDDAddressPage accepts only the canonical address page", () => {
  assert.equal(isPDDAddressPage("https://mobile.pinduoduo.com/addresses.html?refer_page=personal"), true);
  assert.equal(isPDDAddressPage("http://mobile.pinduoduo.com/addresses.html"), false);
  assert.equal(isPDDAddressPage("https://mobile.pinduoduo.com/goods.html"), false);
  assert.equal(isPDDAddressPage("https://mobile.pinduoduo.com.example.com/addresses.html"), false);
});

test("collectDefaultAddressID selects the marked default instead of the first address", () => {
  const rawData = { store: { addressList: [
    { addressId: "58483258420", isDefault: "0" },
    { addressId: "60984097534", isDefault: "1" }
  ] } };
  assert.equal(collectDefaultAddressID(rawData), "60984097534");
});

test("collectDefaultAddressID accepts supported default flag representations", () => {
  for (const isDefault of ["1", 1, true]) {
    assert.equal(collectDefaultAddressID({ store: { addressList: [{ addressId: "60984097534", isDefault }] } }), "60984097534");
  }
});

test("collectDefaultAddressID rejects missing, ambiguous and invalid defaults", () => {
  assert.throws(() => collectDefaultAddressID({ store: {} }), /ADDRESS_DATA_MISSING/);
  assert.throws(() => collectDefaultAddressID({ store: { addressList: [{ addressId: "1", isDefault: "0" }] } }), /DEFAULT_ADDRESS_MISSING/);
  assert.throws(() => collectDefaultAddressID({ store: { addressList: [{ addressId: "1", isDefault: "1" }, { addressId: "2", isDefault: 1 }] } }), /DEFAULT_ADDRESS_AMBIGUOUS/);
  assert.throws(() => collectDefaultAddressID({ store: { addressList: [{ addressId: "not-an-id", isDefault: true }] } }), /DEFAULT_ADDRESS_ID_INVALID/);
});

test("collectMallSN extracts the decoded mall_sn from pddRoute", () => {
  const mallSN = "CgI2WRIIS0xJNU5SeWUaEC2sGini4/ClafQ5Mz0xMQsiEA==";
  const store = { nested: { pddRoute: `mall_page.html?mall_sn=${encodeURIComponent(mallSN)}&decrypt_mall_sn=1` } };
  assert.deepEqual(collectMallSN(store), { value: mallSN, path: "store.nested.pddRoute.mall_sn" });
});

test("collectPDDProduct includes the active tab URL, mall_sn and capped-stock semantics", () => {
  globalThis.location = { href: "chrome-extension://extension/background.js" };
  const pageURL = "https://mobile.pinduoduo.com/goods.html?goods_id=123&page_from=31&_oak_rcto=fresh";
  const result = collectPDDProduct({ store: { goodsID: 123, goodsName: "测试商品", skus: [{ skuId: 456, goodsId: 123, quantity: 1000, limitQuantity: 999999, specs: [{ spec_key: "款式", spec_value: "A" }] }], mall: { pddRoute: "mall_page.html?mall_sn=mall-token&decrypt_mall_sn=1" } } }, pageURL);
  assert.equal(result.goods.mall_sn, "mall-token");
  assert.equal(result.mall_sn_source_path, "store.mall.pddRoute.mall_sn");
  assert.equal(result.final_url, pageURL);
  assert.equal(result.skus[0].stock, 1000);
  assert.equal(result.skus[0].stock_exact, false);
});
