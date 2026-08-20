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

  const stock = asNumber(firstValue(sku, ["quantity", "stock", "initQuantity"]));
  const limitQuantity = asNumber(firstValue(sku, ["limitQuantity", "limit_quantity"]));
  const stockExact = !(stock >= 1000 && limitQuantity > stock);
  return {
    sku_id: skuID,
    goods_id: goodsID,
    thumb_url: asString(firstValue(sku, ["thumbUrl", "thumbURL", "thumb_url"])),
    stock,
    stock_exact: stockExact,
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

export function isPDDAddressPage(pageURL) {
  try {
    const parsed = new URL(asString(pageURL));
    return parsed.protocol === "https:"
      && ["mobile.pinduoduo.com", "mobile.yangkeduo.com"].includes(parsed.hostname)
      && parsed.pathname === "/addresses.html";
  } catch {
    return false;
  }
}

export function collectDefaultAddressID(rawData) {
  const addressLists = [];
  walkObjects(rawData, 7, (value) => {
    if (!Array.isArray(value) || value.length === 0) return;
    const hasAddressShape = value.some((address) => address && typeof address === "object"
      && firstValue(address, ["addressId", "address_id", "id"]) !== undefined
      && firstValue(address, ["isDefault", "is_default", "default", "isDefaultAddress"]) !== undefined);
    if (hasAddressShape) addressLists.push(value);
  });
  if (addressLists.length === 0) {
    throw new Error("ADDRESS_DATA_MISSING: 页面中未找到地址列表，请等待地址页加载完成后重试");
  }

  const defaults = addressLists.flat().filter((address) => {
    const value = firstValue(address, ["isDefault", "is_default", "default", "isDefaultAddress"]);
    return value === "1" || value === 1 || value === true;
  });
  const defaultIDs = [...new Set(defaults.map((address) => asString(firstValue(address, ["addressId", "address_id", "id"]))).filter(Boolean))];
  if (defaultIDs.length === 0) {
    throw new Error("DEFAULT_ADDRESS_MISSING: 未找到默认收货地址，请先在拼多多设置一个默认地址");
  }
  if (defaultIDs.length > 1) {
    throw new Error("DEFAULT_ADDRESS_AMBIGUOUS: 页面返回了多个默认收货地址，请刷新页面后重试");
  }

  const addressID = defaultIDs[0];
  if (!/^\d+$/.test(addressID)) {
    throw new Error("DEFAULT_ADDRESS_ID_INVALID: 默认收货地址缺少有效的地址 ID");
  }
  return addressID;
}

export function isPDDCommentsPage(pageURL) {
  try {
    const parsed = new URL(asString(pageURL));
    return parsed.protocol === "https:" && ["mobile.pinduoduo.com", "mobile.yangkeduo.com"].includes(parsed.hostname)
      && parsed.pathname === "/goods_comments.html" && /^\d+$/.test(parsed.searchParams.get("goods_id") || "");
  } catch { return false; }
}

export function collectPDDReviewMedia(rawData, pageURL) {
  if (!isPDDCommentsPage(pageURL)) throw new Error("COMMENTS_PAGE_REQUIRED: 请先打开拼多多商品评论页");
  const store = rawData?.store;
  if (!store || !Array.isArray(store.commentsList)) throw new Error("COMMENTS_DATA_MISSING: 页面评论尚未加载，请稍后重试");
  const goodsID = asString(store.goodsId || new URL(pageURL).searchParams.get("goods_id"));
  if (!/^\d+$/.test(goodsID) || goodsID !== new URL(pageURL).searchParams.get("goods_id")) throw new Error("COMMENTS_GOODS_ID_INVALID: 评论商品 ID 与页面不一致");
  const media = [];
  const seen = new Set();
  const add = (entry, comment, sourceType, fallbackType = "") => {
    const livePhoto = entry?.live_photo === true || entry?.livePhoto === true;
    const type = livePhoto ? "image" : entry?.type === 0 || fallbackType === "video" ? "video" : "image";
    const url = asString(type === "video" ? (entry?.src || entry?.url) : entry?.url);
    if (!/^https?:\/\//.test(url)) return;
    const md5 = asString(entry?.pic_md5 || entry?.picMd5);
    const key = `${type}:${md5 || url}`;
    if (seen.has(key)) return;
    seen.add(key);
    media.push({
      review_id: asString(comment?.reviewId), sku_id: asString(comment?.skuId), media_key: key,
      media_type: type, source_type: sourceType, remote_url: url,
      cover_url: type === "video" ? asString(entry?.cover_url || entry?.coverUrl || entry?.url) : "",
      media_md5: md5, width: asNumber(entry?.width), height: asNumber(entry?.height),
      duration_ms: asNumber(entry?.duration_ms || entry?.duration), is_live_photo_image: livePhoto
    });
  };
  for (const comment of store.commentsList) {
    const primary = Array.isArray(comment?.picAndVideo) ? comment.picAndVideo : [];
    for (const entry of primary) add(entry, comment, "initial");
    for (const entry of Array.isArray(comment?.additionalPicAndVideo) ? comment.additionalPicAndVideo : []) add(entry, comment, "additional");
    if (!primary.some(entry => entry?.type !== 0)) for (const entry of Array.isArray(comment?.pictures) ? comment.pictures : []) add(entry, comment, "initial", "image");
  }
  return {
    schema_version: 1, collection_id: crypto.randomUUID(), collection_method: "page_raw_data",
    goods_id: goodsID, final_url: pageURL, page_number: asNumber(store.pageNumber),
    no_more_comments: store.noMoreComments === true, media, collected_at: new Date().toISOString()
  };
}

export function collectPDDProduct(rawDataOverride, pageURL = globalThis.location?.href || "") {
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
    final_url: asString(pageURL),
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
