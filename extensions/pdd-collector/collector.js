function asString(value) {
  if (value === undefined || value === null) return "";
  return String(value);
}

function asNumber(value, fallback = 0) {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : fallback;
}

function firstValue(source, keys) {
  for (const key of keys) {
    const value = source?.[key];
    if (value !== undefined && value !== null && value !== "") return value;
  }
  return undefined;
}

function uniqueStrings(values) {
  return [...new Set(values.map(asString).filter(Boolean))];
}

function imageURL(value) {
  if (typeof value === "string") return value;
  return asString(firstValue(value, ["url", "imageUrl", "image_url", "thumbUrl"]));
}

function collectImages(store) {
  const candidates = [
    ...(Array.isArray(store?.goodsGallery) ? store.goodsGallery : []),
    ...(Array.isArray(store?.gallery) ? store.gallery : []),
    ...(Array.isArray(store?.topGallery) ? store.topGallery : []),
    ...(Array.isArray(store?.goodsCarousel) ? store.goodsCarousel : [])
  ];
  const single = [store?.goodsImage, store?.thumbUrl, store?.hdThumbUrl];
  return uniqueStrings([...candidates.map(imageURL), ...single]);
}

function normalizeSpec(spec) {
  return {
    spec_key: asString(firstValue(spec, ["spec_key", "specKey", "key"])),
    spec_key_id: asString(firstValue(spec, ["spec_key_id", "specKeyId", "keyId"])),
    spec_value_id: asString(firstValue(spec, ["spec_value_id", "specValueId", "valueId"])),
    raw_value: asString(firstValue(spec, ["spec_value", "specValue", "value"]))
  };
}

function normalizeSKU(sku, fallbackGoodsID) {
  const skuID = asString(firstValue(sku, ["skuId", "skuID", "sku_id"]));
  const goodsID = asString(firstValue(sku, ["goodsId", "goodsID", "goods_id"]) ?? fallbackGoodsID);
  const specs = (Array.isArray(sku?.specs) ? sku.specs : []).map(normalizeSpec);
  const specValueIDs = uniqueStrings([
    ...specs.map((spec) => spec.spec_value_id),
    ...asString(sku?.spec).split(",")
  ]);

  return {
    sku_id: skuID,
    goods_id: goodsID,
    thumb_url: asString(firstValue(sku, ["thumbUrl", "thumbURL", "thumb_url"])),
    stock: asNumber(firstValue(sku, ["quantity", "stock", "initQuantity"])),
    is_onsale: asNumber(firstValue(sku, ["isOnsale", "is_onsale"]), 1) === 1,
    prices: {
      group_price: firstValue(sku, ["groupPrice", "group_price"]) ?? null,
      old_group_price: firstValue(sku, ["oldGroupPrice", "old_group_price"]) ?? null,
      sku_price: firstValue(sku, ["skuPrice", "sku_price"]) ?? null,
      normal_price: firstValue(sku, ["normalPrice", "normal_price"]) ?? null
    },
    spec_value_ids: specValueIDs,
    specs
  };
}

export function collectPDDProduct(rawDataOverride) {
  const rawData = rawDataOverride ?? globalThis.rawData;
  const store = rawData?.store;
  if (!store) {
    throw new Error("RAW_DATA_MISSING: 页面中未找到 window.rawData.store，请等待商品加载完成后重试");
  }

  const rawSKUs = Array.isArray(store.skus) ? store.skus : [];
  if (rawSKUs.length === 0) {
    throw new Error("SKU_DATA_EMPTY: window.rawData.store.skus 为空");
  }

  const goodsID = asString(firstValue(store, ["goodsId", "goodsID", "goods_id"]) ?? firstValue(rawSKUs[0], ["goodsId", "goodsID", "goods_id"]));
  if (!goodsID) throw new Error("GOODS_ID_MISSING: 未找到 goodsId");

  const skus = rawSKUs.map((sku) => normalizeSKU(sku, goodsID));
  if (skus.some((sku) => !sku.sku_id)) throw new Error("SKU_ID_MISSING: 部分 SKU 缺少 skuId/skuID");

  return {
    schema_version: 1,
    collection_method: "page_raw_data",
    collected_at: new Date().toISOString(),
    final_url: location.href,
    goods: {
      goods_id: goodsID,
      title: asString(firstValue(store, ["goodsName", "goods_name", "title"])),
      images: collectImages(store)
    },
    skus
  };
}
