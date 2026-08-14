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

function walkObjects(root, maxDepth, visit) {
  const queue = [{ value: root, path: "store", depth: 0 }];
  const visited = new Set();
  while (queue.length) {
    const current = queue.shift();
    if (!current?.value || typeof current.value !== "object" || current.depth > maxDepth || visited.has(current.value)) continue;
    visited.add(current.value);
    visit(current.value, current.path);
    for (const [key, value] of Object.entries(current.value)) {
      if (value && typeof value === "object") queue.push({ value, path: `${current.path}.${key}`, depth: current.depth + 1 });
    }
  }
}

function findNestedValue(root, keys) {
  let match;
  walkObjects(root, 6, (value, path) => {
    if (match || Array.isArray(value)) return;
    for (const key of keys) {
      const candidate = value[key];
      if (candidate !== undefined && candidate !== null && candidate !== "") {
        match = { value: candidate, path: `${path}.${key}` };
        return;
      }
    }
  });
  return match ?? { value: undefined, path: "" };
}

function collectImages(store) {
  const fallbackImages = [];
  const paths = [];
  let topGallery = [];
  let detailGallery = [];
  const collectionKeys = new Set(["goodsGallery", "goods_gallery", "gallery", "topGallery", "goodsCarousel", "carouselGallery"]);
  const singleKeys = new Set(["goodsImage", "goods_image", "thumbUrl", "thumbURL", "thumb_url", "hdThumbUrl", "hd_thumb_url"]);
  walkObjects(store, 6, (value, path) => {
    if (Array.isArray(value)) return;
    for (const [key, candidate] of Object.entries(value)) {
      if (collectionKeys.has(key) && Array.isArray(candidate)) {
        const urls = candidate.map(imageURL).filter(Boolean);
        if (urls.length) paths.push(`${path}.${key}`);
        fallbackImages.push(...urls);
        if (key === "topGallery" && urls.length > topGallery.length) topGallery = urls;
      } else if (key === "detailGallery" && Array.isArray(candidate)) {
        const urls = candidate.map(imageURL).filter(Boolean);
        if (urls.length) paths.push(`${path}.${key}`);
        if (urls.length > detailGallery.length) detailGallery = urls;
      } else if (singleKeys.has(key)) {
        const url = imageURL(candidate);
        if (url) {
          paths.push(`${path}.${key}`);
          fallbackImages.push(url);
        }
      }
    }
  });
  const selected = [
    ...topGallery.slice(0, 3),
    ...detailGallery.slice(0, 3),
    ...topGallery.slice(-3)
  ];
  const images = uniqueStrings(selected);
  for (const url of uniqueStrings([...topGallery, ...detailGallery, ...fallbackImages])) {
    if (images.length >= 9) break;
    if (!images.includes(url)) images.push(url);
  }
  return { images: images.slice(0, 9), paths: uniqueStrings(paths) };
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

function collectGoodsProperties(store) {
  let match;
  walkObjects(store, 6, (value, path) => {
    if (match || !Array.isArray(value?.goodsProperty)) return;
    match = { properties: value.goodsProperty, path: `${path}.goodsProperty` };
  });
  const excluded = new Set(["发货地", "品牌"]);
  return {
    path: match?.path || "",
    properties: (match?.properties || []).map(property => ({
      key: asString(property?.key),
      values: uniqueStrings(Array.isArray(property?.values) ? property.values : []),
      ref_pid: asString(firstValue(property, ["ref_pid", "refPid"])),
      reference_id: asString(firstValue(property, ["reference_id", "referenceId"]))
    })).filter(property => property.key && property.values.length && !excluded.has(property.key))
  };
}

export function collectMallSN(store) {
  let match;
  walkObjects(store, 6, (value, path) => {
    if (match || Array.isArray(value)) return;
    const direct = firstValue(value, ["mall_sn", "mallSn"]);
    if (direct !== undefined) {
      match = { value: asString(direct), path: `${path}.mall_sn` };
      return;
    }
    const route = firstValue(value, ["pddRoute", "pdd_route"]);
    if (!route) return;
    try {
      const parsed = new URL(asString(route), "https://mobile.pinduoduo.com/");
      const mallSN = parsed.searchParams.get("mall_sn");
      if (mallSN) match = { value: mallSN, path: `${path}.pddRoute.mall_sn` };
    } catch {
      // Ignore malformed route candidates and continue walking for another one.
    }
  });
  return match ?? { value: "", path: "" };
}

export function collectPDDProduct(rawDataOverride) {
  const rawData = rawDataOverride ?? globalThis.rawData;
  const store = rawData?.store;
  if (!store) {
    throw new Error("RAW_DATA_MISSING: 页面中未找到 window.rawData.store，请等待商品加载完成后重试");
  }

  function findSKUArray(root) {
    let match;
    walkObjects(root, 6, (value, path) => {
      if (!match && Array.isArray(value) && value.some((item) => item && typeof item === "object" && (item.skuId !== undefined || item.skuID !== undefined || item.sku_id !== undefined))) match = { skus: value, path };
    });
    return match ?? { skus: [], path: "" };
  }

  const located = Array.isArray(store.skus) && store.skus.length ? { skus: store.skus, path: "store.skus" } : findSKUArray(store);
  const rawSKUs = located.skus;
  if (rawSKUs.length === 0) {
    const keys = Object.keys(store).slice(0, 30).join(", ");
    throw new Error("SKU_DATA_EMPTY: 未在 rawData.store 中找到 SKU 数组；store字段: " + keys);
  }

  const goodsID = asString(firstValue(store, ["goodsId", "goodsID", "goods_id"]) ?? firstValue(rawSKUs[0], ["goodsId", "goodsID", "goods_id"]));
  if (!goodsID) throw new Error("GOODS_ID_MISSING: 未找到 goodsId");

  const skus = rawSKUs.map((sku) => normalizeSKU(sku, goodsID));
  if (skus.some((sku) => !sku.sku_id)) throw new Error("SKU_ID_MISSING: 部分 SKU 缺少 skuId/skuID");
  const title = findNestedValue(store, ["goodsName", "goods_name", "goodsTitle", "goods_title", "title"]);
  const imageResult = collectImages(store);
  const propertyResult = collectGoodsProperties(store);
  const mallSN = collectMallSN(store);

  return {
    schema_version: 1,
    collection_method: "page_raw_data",
    sku_source_path: located.path,
    title_source_path: title.path,
    image_source_paths: imageResult.paths,
    goods_property_source_path: propertyResult.path,
    mall_sn_source_path: mallSN.path,
    collected_at: new Date().toISOString(),
    final_url: location.href,
    goods: {
      goods_id: goodsID,
      mall_sn: mallSN.value,
      title: asString(title.value),
      images: imageResult.images,
      goods_property: propertyResult.properties
    },
    skus
  };
}
