import assert from "node:assert/strict";
import test from "node:test";

import { collectDefaultAddressID, collectMallSN, collectPDDProduct, collectPDDReviewMedia, isPDDAddressPage } from "./collector.js";

test("collectPDDReviewMedia stores live photos only as images and keeps normal videos separate", () => {
  const url = "https://mobile.pinduoduo.com/goods_comments.html?goods_id=977731220380";
  const result = collectPDDReviewMedia({ store: { goodsId: "977731220380", pageNumber: 2, noMoreComments: false, commentsList: [{ reviewId: "r1", skuId: 12, pictures: [{ url: "https://review/a.jpg", picMd5: "a" }], picAndVideo: [{ type: 1, url: "https://review/a.jpg", pic_md5: "a" }, { type: 0, src: "https://video/v.mp4" }, { type: 1, url: "https://review/live.jpg", live_photo: true, live_photo_video: { url: "https://video/live.mp4" } }] }] } }, url);
  assert.deepEqual(result.media.map(item => [item.media_type, item.remote_url]), [["image", "https://review/a.jpg"], ["video", "https://video/v.mp4"], ["image", "https://review/live.jpg"]]);
  assert.equal(result.media[2].is_live_photo_image, true);
  assert.equal(result.media.some(item => item.remote_url === "https://video/live.mp4"), false);
});

test("collectPDDReviewMedia supports camelCase live-photo fields from pictures", () => {
  const url = "https://mobile.pinduoduo.com/goods_comments.html?goods_id=920525700280";
  const result = collectPDDReviewMedia({ store: { goodsId: "920525700280", commentsList: [{
    reviewId: "r2", skuId: 34,
    pictures: [{ url: "https://review/live-camel.jpg", livePhoto: true, livePhotoVideo: { url: "https://video/live-camel.mp4" } }],
    picAndVideo: []
  }] } }, url);
  assert.deepEqual(result.media.map(item => [item.media_type, item.remote_url]), [["image", "https://review/live-camel.jpg"]]);
  assert.equal(result.media[0].is_live_photo_image, true);
});

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

test("collectDefaultAddressID supports nested address stores and snake_case fields", () => {
  const rawData = {
    addressesStore: {
      loaded: true,
      addresses: [
        { address_id: 60984097533, is_default: 0 },
        { address_id: 60984097534, is_default: 1 }
      ]
    }
  };
  assert.equal(collectDefaultAddressID(rawData), "60984097534");
});

test("collectDefaultAddressID de-duplicates the same default address across page stores", () => {
  const address = { addressId: "60984097534", isDefault: true };
  assert.equal(collectDefaultAddressID({ store: { addressList: [address] }, addressStore: { addresses: [address] } }), "60984097534");
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
