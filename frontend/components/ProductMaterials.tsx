import React, { useEffect, useMemo, useState } from 'react';
import { ArrowDown, ArrowLeft, ArrowRight, ArrowUp, Copy, ImagePlus, PackagePlus, Play, Plus, Save, Scissors, Search, Send, Trash2, X } from 'lucide-react';
import {
  deleteMaterial, getAccountDetails, getMaterials, getMaterialPublishRecords, getMaterialSourceDiff, MaterialPublishRecord, ProductMaterial, ProductMaterialSKU,
  getPDDProduct, getPDDReviewMedia, PDDReviewMedia, ProductMaterialVideo, publishMaterial, syncMaterialSource, updateMaterial, uploadMaterialImage,
	updateMaterialSKUSource,
	splitMaterial,
} from '../services/api';
import type {PublishLocation} from '../services/api';
import {applyShortcutPlans, buildShortcutSplitPlans, clearSplitPreview, MATERIAL_SKU_GROUP_LIMIT, removeSplitPreviewGroup} from '../services/materialSplit';
import {PublishLocationPicker} from './PublishLocationPicker';
import type { AccountDetail } from '../types';

type EditorMode = 'edit' | 'publish';
type Specification = { name: string; supportImage: boolean; values: Array<{ value: string; image_url?: string }> };

const clone = <T,>(value: T): T => structuredClone(value);
const money = (cents: number) => (cents / 100).toFixed(2);
const pddNormalPriceCent = (prices: Record<string, unknown>) => {
  const value = prices.normal_price;
  const yuan = typeof value === 'number' ? value : typeof value === 'string' ? Number(value) : 0;
  return Number.isFinite(yuan) && yuan > 0 ? Math.round(yuan * 100) : 0;
};
const grossProfit = (sku: ProductMaterialSKU) => sku.source_price_cent && sku.source_price_cent > 0 ? sku.price_cent - sku.source_price_cent : null;
const grossMargin = (sku: ProductMaterialSKU) => {
  const profit = grossProfit(sku);
  return profit === null || sku.price_cent <= 0 ? null : profit / sku.price_cent * 100;
};
const PriceInput = ({ cents, onChange }: { cents: number; onChange: (cents: number) => void }) => {
  const [text, setText] = useState(() => cents > 0 ? money(cents) : '');
  const [editing, setEditing] = useState(false);
  useEffect(() => { if (!editing) setText(cents > 0 ? money(cents) : ''); }, [cents, editing]);
  return <input type="number" min="0.01" step="0.01" className="w-28 rounded-lg border p-2" value={text}
    onFocus={() => setEditing(true)}
    onChange={event => {
      const next = event.target.value;
      setText(next);
      if (next === '') return onChange(0);
      const value = Number(next);
      if (Number.isFinite(value)) onChange(Math.round(value * 100));
    }}
    onBlur={() => {
      setEditing(false);
      if (cents > 0) setText(money(cents));
    }}/>
};
const deriveSpecifications = (skus: ProductMaterialSKU[]): Specification[] => {
  const result: Specification[] = [];
  for (const sku of skus) for (const property of sku.properties) {
    let specification = result.find(item => item.name === property.name);
    if (!specification) { specification = { name: property.name, supportImage: false, values: [] }; result.push(specification); }
    if (!specification.values.some(item => item.value === property.value)) specification.values.push({ value: property.value, image_url: property.image_url });
    if (property.image_url) specification.supportImage = true;
  }
	let imageSpecificationIndex = -1;
	for (let index = result.length - 1; index >= 0; index--) if (result[index].supportImage) { imageSpecificationIndex = index; break; }
	for (const [index, specification] of result.entries()) if (specification.supportImage && index !== imageSpecificationIndex) {
		specification.supportImage = false;
		specification.values = specification.values.map(value => ({ ...value, image_url: undefined }));
	}
  return result.slice(0, 2);
};
const skuKey = (properties: Array<{ name: string; value: string }>) => properties.map(item => `${item.name}=${item.value}`).join('\0');
export const sameSpecificationShape = (before: Specification[], after: Specification[]) => before.length === after.length && before.every((item, index) => item.values.length === after[index]?.values.length);
// A specification label/value edit is a rename, not a new SKU matrix. Preserve the
// row identity and source binding by translating every property by its stable position.
export const renameSKUProperties = (skus: ProductMaterialSKU[], before: Specification[], after: Specification[]): ProductMaterialSKU[] => skus.map(sku => ({
  ...sku,
  properties: sku.properties.map(property => {
    const specificationIndex = before.findIndex(item => item.name === property.name);
    if (specificationIndex < 0) return property;
    const valueIndex = before[specificationIndex].values.findIndex(item => item.value === property.value);
    const nextSpecification = after[specificationIndex];
    const nextValue = nextSpecification?.values[valueIndex];
    if (!nextSpecification || !nextValue) return property;
    return { name: nextSpecification.name, value: nextValue.value, image_url: nextSpecification.supportImage ? nextValue.image_url : undefined };
  }),
}));
const generateSKUs = (specifications: Specification[], previous: ProductMaterialSKU[], sourceType: string): ProductMaterialSKU[] => {
  if (!specifications.length || specifications.some(item => !item.name.trim() || !item.values.length)) return previous;
  const combinations = specifications.reduce<Array<Array<{ name: string; value: string; image_url?: string }>>>((rows, specification) => rows.flatMap(row => specification.values.filter(item => item.value.trim()).map(item => [...row, { name: specification.name, value: item.value, image_url: specification.supportImage ? item.image_url : undefined }])), [[]]);
  const previousByKey = new Map(previous.map(item => [skuKey(item.properties), item]));
  return combinations.slice(0, 200).map(properties => {
    const projectedMatches = previous.filter(sku => properties.every(property => sku.properties.some(old => old.name === property.name && old.value === property.value)));
    const matched = previousByKey.get(skuKey(properties)) || (projectedMatches.length === 1 ? projectedMatches[0] : undefined);
    return matched ? { ...matched, properties } : { sku_type: sourceType === 'pdd' ? 'placeholder' : 'manual', price_cent: previous[0]?.price_cent || 100, quantity: 0, enabled: true, properties };
  });
};

const upsertDescriptionSection = (description: string, title: string, lines: string[]) => {
  const heading = `【${title}】`;
  const section = `${heading}\n${lines.join('\n')}`;
  const escaped = heading.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const pattern = new RegExp(`(?:\\n\\n)?${escaped}\\n[\\s\\S]*?(?=\\n\\n【|$)`);
  const base = description.replace(pattern, '').trim();
  return [base, section].filter(Boolean).join('\n\n').slice(0, 1500);
};

function ProductEditor({ initial, mode, accounts, onClose, onSaved }: {
  initial: ProductMaterial; mode: EditorMode; accounts: AccountDetail[];
  onClose: () => void; onSaved: () => Promise<void>;
}) {
  const [draft, setDraft] = useState(() => {
	const value = clone(initial);
	value.original_price_cent ||= 0;
	value.source_properties ||= [];
	value.image_metadata ||= value.images.map(url => ({url,source:'legacy',status:'valid'}));
	value.price_strategy ||= {mode:'manual',value:0,minimum_profit_cent:50};
	value.stock_strategy ||= {mode:'manual',cap:0,reserve:0,fixed_quantity:0,disable_when_oos:true};
	value.revision ||= 1;
	value.skus = value.skus.map(sku => {
		const sku_type = sku.sku_type || (sku.source_sku_id ? 'source' : value.source_type === 'pdd' && sku.quantity === 0 ? 'placeholder' : 'manual');
		return sku_type === 'placeholder' ? { ...sku, sku_type, quantity: 0 } : { ...sku, sku_type };
	});
	return value;
  });
  const [cookieID, setCookieID] = useState(accounts.find(account => account.enabled)?.id || '');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [publishLocation, setPublishLocation] = useState<PublishLocation | null>(null);
	const [priceAdjustment, setPriceAdjustment] = useState('');
  const [batchStock, setBatchStock] = useState('');
	const [publishRecords, setPublishRecords] = useState<MaterialPublishRecord[]>([]);
	const [sourceDiff, setSourceDiff] = useState<{added:any[];changed:any[];removed:string[]}|null>(null);
	const [combineGoodsID, setCombineGoodsID] = useState('');
	const [combining, setCombining] = useState(false);
	const [descriptionBusy, setDescriptionBusy] = useState(false);
	const [productImageChoices, setProductImageChoices] = useState<Array<{url:string;goods_id:string}>>([]);
	const [reviewImageChoices, setReviewImageChoices] = useState<PDDReviewMedia[]>([]);
	const [reviewVideoChoices, setReviewVideoChoices] = useState<PDDReviewMedia[]>([]);
	const [mediaPicker, setMediaPicker] = useState<null|{kind:'product-image'|'review-image'|'review-video';selected:Set<string>}>(null);
	const [mediaPreview, setMediaPreview] = useState<null|{key?:string;type:'image'|'video';url:string;cover?:string}>(null);
	const [mediaGoodsFilter, setMediaGoodsFilter] = useState('');
	const [mediaSKUFilter, setMediaSKUFilter] = useState('');
	const [mediaSourceFilter, setMediaSourceFilter] = useState('');
  const [specifications, setSpecifications] = useState<Specification[]>(() => deriveSpecifications(initial.skus).map(item => ({ ...item, supportImage: item.name === initial.image_property_name, values: item.values.map(value => ({ ...value, image_url: item.name === initial.image_property_name ? value.image_url : undefined })) })));
  const dimensions = useMemo(() => specifications.map(item => item.name), [specifications]);
	const sourceGoodsIDs = useMemo(() => Array.from(new Set(draft.skus.map(sku => sku.source_goods_id || (sku.source_sku_id ? draft.source_id : '')).filter(Boolean))), [draft.skus, draft.source_id]);
	useEffect(() => {
		const imageName = specifications.find(item => item.supportImage)?.name || '';
		setDraft(current => ({ ...current, skus: current.skus.map(sku => ({ ...sku, properties: sku.properties.map(property => property.name === imageName ? property : { ...property, image_url: undefined }) })) }));
	}, []);
	useEffect(() => { void getMaterialPublishRecords(initial.id).then(setPublishRecords); }, [initial.id]);
	useEffect(() => { setMediaGoodsFilter(''); setMediaSKUFilter(''); setMediaSourceFilter(''); }, [mediaPicker?.kind]);
	useEffect(() => {
		if (initial.source_type !== 'pdd') return;
		void Promise.all(sourceGoodsIDs.map(async goodsID => {
			const [product, images, videos] = await Promise.all([getPDDProduct(goodsID), getPDDReviewMedia(goodsID,'image'), getPDDReviewMedia(goodsID,'video')]);
			return { goodsID, product, images, videos };
		})).then(rows => {
			setProductImageChoices(Array.from(new Map(rows.flatMap(row => row.product.images.map(url => [url,{url,goods_id:row.goodsID}] as const))).values()));
			setReviewImageChoices(rows.flatMap(row=>row.images)); setReviewVideoChoices(rows.flatMap(row=>row.videos));
		}).catch(reason=>setError(reason?.message||'读取采集媒体失败'));
	}, []);
	const unfilteredPickerChoices = mediaPicker?.kind === 'product-image' ? productImageChoices.map(item=>({key:item.url,url:item.url,label:item.goods_id,goodsID:item.goods_id,skuID:'',sourceType:'',type:'image' as const})) : mediaPicker?.kind === 'review-image' ? reviewImageChoices.map(item=>({key:String(item.id),url:item.url,label:`${item.source_type==='additional'?'追评':'评价'} · SKU ${item.sku_id||'-'}`,goodsID:item.goods_id,skuID:item.sku_id,sourceType:item.source_type,type:'image' as const})) : reviewVideoChoices.map(item=>({key:String(item.id),url:item.url,cover:item.cover_url,label:`${item.source_type==='additional'?'追评':'评价'} · SKU ${item.sku_id||'-'}`,goodsID:item.goods_id,skuID:item.sku_id,sourceType:item.source_type,type:'video' as const}));
	const pickerChoices = unfilteredPickerChoices.filter(item=>(!mediaGoodsFilter||item.goodsID===mediaGoodsFilter)&&(!mediaSKUFilter||item.skuID===mediaSKUFilter)&&(!mediaSourceFilter||item.sourceType===mediaSourceFilter));
	const pickerGoodsIDs = Array.from(new Set(unfilteredPickerChoices.map(item=>item.goodsID).filter(Boolean)));
	const pickerSKUIds = Array.from(new Set(unfilteredPickerChoices.filter(item=>!mediaGoodsFilter||item.goodsID===mediaGoodsFilter).map(item=>item.skuID).filter(Boolean)));
	const toggleMediaSelection = (key:string) => setMediaPicker(current=>{
		if (!current) return current;
		const selected = new Set(current.selected);
		selected.has(key) ? selected.delete(key) : selected.add(key);
		return {...current,selected};
	});
	const moveMediaPreview = (direction:-1|1) => {
		if (!mediaPreview?.key || pickerChoices.length < 2) return;
		const index = pickerChoices.findIndex(item=>item.key===mediaPreview.key);
		if (index < 0) return;
		const next = pickerChoices[(index + direction + pickerChoices.length) % pickerChoices.length];
		setMediaPreview({key:next.key,type:next.type,url:next.url,cover:'cover' in next?next.cover:''});
	};
	useEffect(() => {
		if (!mediaPreview) return;
		const onKeyDown = (event:KeyboardEvent) => {
			if (event.key === 'Escape') setMediaPreview(null);
			else if (event.key === 'ArrowLeft') { event.preventDefault(); moveMediaPreview(-1); }
			else if (event.key === 'ArrowRight') { event.preventDefault(); moveMediaPreview(1); }
			else if ((event.key === 'Enter' || event.key === ' ') && mediaPreview.key) { event.preventDefault(); toggleMediaSelection(mediaPreview.key); }
		};
		window.addEventListener('keydown',onKeyDown);
		return () => window.removeEventListener('keydown',onKeyDown);
	}, [mediaPreview,pickerChoices]);
	const confirmMediaPicker = () => {
		if (!mediaPicker) return;
		const selected = unfilteredPickerChoices.filter(item=>mediaPicker.selected.has(item.key));
		if (mediaPicker.kind === 'review-video') {
			const byID = new Map(reviewVideoChoices.map(item=>[String(item.id),item]));
			setDraft(current=>({...current,videos:Array.from(new Map([...(current.videos||[]),...selected.map(item=>{const source=byID.get(item.key)!;return {source:'review',source_goods_id:source.goods_id,review_id:source.review_id,sku_id:source.sku_id,url:source.url,cover_url:source.cover_url,duration_ms:source.duration_ms} as ProductMaterialVideo;})].map(item=>[item.url,item])).values())}));
		} else {
			setDraft(current=>{
				const images=Array.from(new Set([...current.images,...selected.map(item=>item.url)])).slice(0,9);
				const selectedURLs=new Set(selected.map(item=>item.url));
				const source=mediaPicker.kind==='product-image'?'pdd_product':'pdd_review';
				const metadata=Array.from(new Map([...current.image_metadata,...images.filter(url=>selectedURLs.has(url)).map(url=>({url,source,status:'valid'}))].map(item=>[item.url,item])).values()).filter(item=>images.includes(item.url));
				return {...current,images,image_metadata:metadata};
			});
		}
		setMediaPicker(null);
	};

  const applySpecifications = (next: Specification[]) => {
	setDraft(current => {
		if (sameSpecificationShape(specifications, next)) return { ...current, skus: renameSKUProperties(current.skus, specifications, next) };
		if (sourceGoodsIDs.length <= 1) return { ...current, skus: generateSKUs(next, current.skus, current.source_type) };
		const remapped = current.skus.flatMap(sku => {
			const properties = next.map((nextSpec, specIndex) => {
				const previousSpec = specifications[specIndex];
				const previousProperty = previousSpec ? sku.properties.find(property => property.name === previousSpec.name) : undefined;
				if (!previousSpec || !previousProperty) return { name: nextSpec.name, value: nextSpec.values[0]?.value || '' };
				const valueIndex = previousSpec.values.findIndex(value => value.value === previousProperty.value);
				const nextValue = nextSpec.values[valueIndex];
				return nextValue ? { name: nextSpec.name, value: nextValue.value, image_url: nextSpec.supportImage ? nextValue.image_url : undefined } : null;
			});
			return properties.some(property => property === null) ? [] : [{ ...sku, properties: properties as ProductMaterialSKU['properties'] }];
		});
		return { ...current, skus: remapped };
	});
	setSpecifications(next);
  };
	const removeSpecification = (specIndex: number) => {
		const remaining = specifications.filter((_, index) => index !== specIndex);
		if (!remaining.length && draft.skus.length > 1) {
			setError(`不能删除最后一个规格“${specifications[specIndex].name}”：当前仍有 ${draft.skus.length} 个 SKU，请先保留一个 SKU`);
			return;
		}
		if (remaining.length) {
			const projected = draft.skus.map(sku => skuKey(sku.properties.filter(property => remaining.some(spec => spec.name === property.name))));
			if (new Set(projected).size !== projected.length) {
				setError(`删除“${specifications[specIndex].name}”会合并多个不同来源 SKU，请先删除或调整冲突组合`);
				return;
			}
		}
		applySpecifications(remaining);
		setError('');
	};
	const appendCollectedDetails = async () => {
		setDescriptionBusy(true); setError('');
		try {
			const products = draft.source_type === 'pdd' ? await Promise.all(sourceGoodsIDs.map(getPDDProduct)) : [];
			const attributeLines = products.flatMap(product => product.goods_property.map(property => `${products.length > 1 ? `[${product.goods_id}] ` : ''}${property.key}：${property.values.join('、')}`));
			const propertyOrder: string[] = [];
			const propertyValues = new Map<string, string[]>();
			for (const sku of draft.skus) for (const property of (sku.source_properties?.length ? sku.source_properties : sku.properties)) {
				if (!propertyValues.has(property.name)) { propertyValues.set(property.name, []); propertyOrder.push(property.name); }
				const values = propertyValues.get(property.name)!;
				if (property.value.trim() && !values.includes(property.value.trim())) values.push(property.value.trim());
			}
			const specificationLines = propertyOrder.map(name => `${name}：${propertyValues.get(name)!.join('、')}`);
			let description = draft.description;
			if (attributeLines.length) description = upsertDescriptionSection(description, '商品属性', attributeLines);
			if (specificationLines.length) description = upsertDescriptionSection(description, '商品规格', specificationLines);
			setDraft(current => ({ ...current, description }));
			if (!attributeLines.length && !specificationLines.length) setError('当前素材没有可添加的采集属性或规格');
		} catch (reason: any) { setError(reason?.message || '读取采集商品信息失败'); }
		finally { setDescriptionBusy(false); }
	};
	const setImageSpecification = (specIndex: number, enabled: boolean) => {
		const selectedName = specifications[specIndex].name;
		const sourceImageByValue = new Map<string,string>();
		for (const sku of draft.skus) {
			const property = sku.properties.find(item => item.name === selectedName);
			if (property?.value && sku.source_image_url && !sourceImageByValue.has(property.value)) sourceImageByValue.set(property.value, sku.source_image_url);
		}
		const next = specifications.map((item, index) => ({
			...item,
			supportImage: enabled ? index === specIndex : index === specIndex ? false : item.supportImage,
			values: index === specIndex && enabled ? item.values.map(value => ({ ...value, image_url: sourceImageByValue.get(value.value) || value.image_url })) : enabled || index !== specIndex ? item.values : item.values.map(value => ({ ...value, image_url: undefined })),
		}));
		if (enabled) {
			for (const [index, item] of next.entries()) if (index !== specIndex) item.values = item.values.map(value => ({ ...value, image_url: undefined }));
		}
		applySpecifications(next);
		setDraft(current => ({ ...current, image_property_name: enabled ? specifications[specIndex].name : current.image_property_name === specifications[specIndex].name ? '' : current.image_property_name }));
	};

  const patchSKU = (index: number, patch: Partial<ProductMaterialSKU>) => {
    const skus = [...draft.skus];
    skus[index] = { ...skus[index], ...patch };
    setDraft({ ...draft, skus });
  };
	const adjustAllPrices = () => {
		const adjustment = Math.round(Number(priceAdjustment) * 100);
		if (!Number.isFinite(adjustment) || adjustment === 0) return setError('请输入非 0 的调价金额，例如 5 或 -5');
		if (draft.skus.some(sku => sku.price_cent + adjustment <= 0)) return setError('调价后售价必须大于 0');
		setDraft(current => ({ ...current, skus: current.skus.map(sku => ({ ...sku, price_cent: sku.price_cent + adjustment })) }));
		setError('');
	};
	const setAllStock = () => {
		const quantity = Number(batchStock);
		if (!Number.isInteger(quantity) || quantity < 0) return setError('库存必须是大于等于 0 的整数');
		setDraft(current => ({ ...current, skus: current.skus.map(sku => sku.sku_type === 'placeholder' ? { ...sku, quantity: 0 } : { ...sku, quantity }) }));
		setError('');
	};
  const setPropertyImage = (name: string, value: string, imageURL: string) => {
    setSpecifications(current => current.map(specification => specification.name === name ? { ...specification, values: specification.values.map(item => item.value === value ? { ...item, image_url: imageURL } : item) } : specification));
    setDraft(current => ({ ...current, skus: current.skus.map(sku => ({ ...sku, properties: sku.properties.map(property => property.name === name && property.value === value ? { ...property, image_url: imageURL } : property) })) }));
  };
	const combinePDDProduct = async () => {
		const goodsID = combineGoodsID.trim();
		if (!goodsID) return setError('请输入已采集的拼多多商品 ID');
		if (sourceGoodsIDs.includes(goodsID)) return setError('该拼多多商品已经在当前素材中');
		setCombining(true); setError('');
		try {
			const product = await getPDDProduct(goodsID);
			const incoming: ProductMaterialSKU[] = product.skus.map(sku => {
				const properties = sku.specs.map(spec => ({ name: spec.spec_key, value: spec.raw_value }));
				return { sku_type: 'source' as const, source_goods_id: goodsID, source_sku_id: sku.sku_id, source_properties: clone(properties), source_image_url: sku.thumb_url, image_url: sku.thumb_url, source_price_cent: sku.price_cent, source_normal_price_cent: pddNormalPriceCent(sku.prices), source_price_updated_at: sku.last_collected_at, source_price_origin: 'collected' as const, price_cent: sku.price_cent, quantity: sku.stock, enabled: sku.is_onsale, properties };
			});
			if (!incoming.length) throw new Error('该采集商品没有可合并的 SKU');
			const names = Array.from(new Set([...draft.skus, ...incoming].flatMap(sku => sku.properties.map(property => property.name))));
			if (names.length > 2) throw new Error(`合并后共有 ${names.length} 个规格类型，闲鱼最多支持 2 个；请先统一来源商品的规格类型`);
			if (draft.skus.length + incoming.length > 5000) throw new Error('合并后 SKU 超过本地安全上限 5000 行');
			const keys = new Set(draft.skus.map(sku => skuKey(sku.properties)));
			if (incoming.some(sku => keys.has(skuKey(sku.properties)))) throw new Error('来源商品存在相同规格组合，请先修改当前素材的发布规格值再合并');
			const merged = [...draft.skus, ...incoming];
			setDraft(current => {
				const images=Array.from(new Set([...current.images,...product.images])).slice(0,9);
				const sourceProperties=Array.from(new Map([...current.source_properties,...product.goods_property.map(property=>({name:property.key,values:property.values}))].map(property=>[property.name,property])).values());
				const metadata=Array.from(new Map([...current.image_metadata,...product.images.map(url=>({url,source:'pdd_product',status:'valid'}))].map(item=>[item.url,item])).values()).filter(item=>images.includes(item.url));
				return {...current,source_ids:[...sourceGoodsIDs,goodsID],skus:merged,images,source_properties:sourceProperties,image_metadata:metadata};
			});
			setSpecifications(deriveSpecifications(merged));
			setCombineGoodsID('');
		} catch (reason: any) { setError(reason?.message || '合并采集商品失败'); }
		finally { setCombining(false); }
	};
	const removePDDSource = (goodsID: string) => {
		const remaining = draft.skus.filter(sku => (sku.source_goods_id || (sku.source_sku_id ? draft.source_id : '')) !== goodsID);
		if (!remaining.length) return setError('素材至少需要保留一个 SKU 来源');
			setDraft(current => ({ ...current, source_ids: sourceGoodsIDs.filter(id => id !== goodsID), skus: remaining }));
		setSpecifications(deriveSpecifications(remaining));
		setError('');
	};
	const changeSKUSource = async (index: number, action: 'bind'|'convert_manual'|'convert_placeholder') => {
		const sku = draft.skus[index];
		if (!sku.material_sku_id) return setError('请先保存素材，再修改 SKU 来源');
		let source_goods_id = '', source_sku_id = '';
		if (action === 'bind') {
			source_goods_id = window.prompt('输入已采集的拼多多 goods_id', sku.source_goods_id || draft.source_id) || '';
			if (!source_goods_id.trim()) return;
			source_sku_id = window.prompt('输入对应的拼多多 sku_id', sku.source_sku_id || '') || '';
			if (!source_sku_id.trim()) return;
		} else if (!window.confirm(action === 'convert_manual' ? '确认解除拼多多来源并转为手工 SKU？' : '确认转为库存强制为 0 的占位 SKU？')) return;
		setBusy(true); setError('');
		try {
			const result = await updateMaterialSKUSource(draft.id, sku.material_sku_id, {action, source_goods_id, source_sku_id});
			setDraft(current => ({...current, skus: current.skus.map((row, rowIndex) => rowIndex === index ? result.sku : row)}));
		} catch (reason: any) { setError(reason?.message || '修改 SKU 来源失败'); }
		finally { setBusy(false); }
	};
  const validate = (forPublish = false) => {
    if (!draft.title.trim()) return '请填写商品标题';
    if (!draft.description.trim()) return '请填写商品描述';
    if (draft.images.length < 1 || draft.images.length > 9) return '商品图片必须为 1 到 9 张';
    if (draft.skus.length < 1 || draft.skus.length > 5000) return 'SKU 必须为 1 到 5000 行';
    if (draft.skus.some(sku => sku.price_cent <= 0 || sku.quantity < 0 || !sku.properties.length)) return '请完善所有 SKU 的规格、价格和库存';
	const minimumPrice = Math.min(...draft.skus.filter(sku=>sku.enabled!==false).map(sku=>sku.price_cent));
	if (draft.original_price_cent > 0 && draft.original_price_cent < minimumPrice) return '闲鱼原价不能低于最低闲鱼售价';
    const keys = draft.skus.map(sku => sku.properties.map(property => `${property.name}=${property.value}`).join('\0'));
    if (new Set(keys).size !== keys.length) return '存在重复的 SKU 规格组合';
	const publishedSKUs = draft.skus.filter(sku => sku.enabled !== false);
	if (forPublish && publishedSKUs.length > 252) return `闲鱼单个商品最多发布 252 个启用的 SKU，当前为 ${publishedSKUs.length} 个`;
	if (publishedSKUs.length > 1) {
		const values = new Map<string, Set<string>>();
		for (const sku of publishedSKUs) for (const property of sku.properties) {
			if (!values.has(property.name)) values.set(property.name, new Set());
			values.get(property.name)!.add(property.value.trim());
		}
		for (const [name, set] of values) if (set.size < 2 || set.size > 150) return `规格“${name}”必须包含 2 到 150 个不同规格值，当前为 ${set.size} 个`;
	}
    return '';
  };
  const save = async () => {
    const message = validate(); if (message) return setError(message);
    setBusy(true); setError('');
    try {
      const result = await updateMaterial(draft.id, {
        title: draft.title, description: draft.description, images: draft.images,
        category: draft.category, skus: draft.skus, postage_mode: draft.postage_mode,
        postage_cent: draft.postage_cent,
		image_property_name: draft.image_property_name || '',
		video_enabled: draft.video_enabled !== false, videos: draft.videos || [],
		original_price_cent:draft.original_price_cent,source_properties:draft.source_properties||[],image_metadata:draft.image_metadata||[],price_strategy:draft.price_strategy,stock_strategy:draft.stock_strategy,revision:draft.revision,
      });
	  setDraft(current=>({...current,revision:result.revision}));
      await onSaved();
      if (mode === 'edit') onClose();
    } catch (reason: any) { setError(reason?.message || '保存失败'); }
    finally { setBusy(false); }
  };
  const publish = async () => {
    const message = validate(true); if (message) return setError(message);
    if (!cookieID) return setError('请选择发布账号');
    setBusy(true); setError('');
    try {
      const saved = await updateMaterial(draft.id, {
        title: draft.title, description: draft.description, images: draft.images,
        category: draft.category, skus: draft.skus, postage_mode: draft.postage_mode,
        postage_cent: draft.postage_cent,
		image_property_name: draft.image_property_name || '',
		video_enabled: draft.video_enabled !== false, videos: draft.videos || [],
		original_price_cent:draft.original_price_cent,source_properties:draft.source_properties||[],image_metadata:draft.image_metadata||[],price_strategy:draft.price_strategy,stock_strategy:draft.stock_strategy,revision:draft.revision,
      });
	  setDraft(current=>({...current,revision:saved.revision}));
      const result = await publishMaterial(draft.id, cookieID, publishLocation || undefined);
      alert(`商品发布成功${result?.item_id ? `，ID：${result.item_id}` : ''}`);
      await onSaved(); onClose();
    } catch (reason: any) { setError(reason?.message || '发布失败'); }
    finally { setBusy(false); }
  };

  return <div className="fixed inset-0 z-50 overflow-y-auto bg-slate-100">
    <header className="sticky top-0 z-10 flex items-center justify-between border-b bg-white px-6 py-4 shadow-sm">
      <div><h2 className="text-xl font-black">{mode === 'publish' ? '发布商品' : '编辑素材'}</h2><p className="text-xs text-slate-500">同一份商品内容用于素材保存和正式发布</p></div>
      <button onClick={onClose} className="rounded-lg p-2 hover:bg-slate-100"><X /></button>
    </header>
    <main className="mx-auto grid max-w-7xl gap-5 p-5 xl:grid-cols-[minmax(0,1fr)_340px]">
      <div className="space-y-5">
	        <section className="rounded-2xl border bg-white p-5 shadow-sm"><h3 className="mb-4 font-black">基础信息</h3>
          <label className="mb-4 block text-sm font-bold">商品标题<input maxLength={60} className="mt-2 w-full rounded-xl border p-3 font-normal" value={draft.title} onChange={event => setDraft({ ...draft, title: event.target.value })}/></label>
          <label className="block text-sm font-bold">商品描述<textarea rows={7} maxLength={1500} className="mt-2 w-full rounded-xl border p-3 font-normal" value={draft.description} onChange={event => setDraft({ ...draft, description: event.target.value })}/></label>
		  <div className="mt-3 flex items-center gap-3"><button type="button" disabled={descriptionBusy} className="rounded-lg border bg-white px-3 py-2 text-sm font-bold text-brand disabled:opacity-50" onClick={()=>void appendCollectedDetails()}>{descriptionBusy?'读取中…':'一键添加商品属性与规格'}</button><span className="text-xs text-slate-400">重复点击会更新对应区块，不会重复追加</span></div>
	        </section>
		<section className="rounded-2xl border bg-white p-5 shadow-sm space-y-4"><div><h3 className="font-black">发布价格与库存策略</h3><p className="mt-1 text-xs text-slate-500">策略作为素材依据保存；当前价格和库存仍逐 SKU 明确提交，不会在后台静默改价。</p></div><div className="grid gap-4 sm:grid-cols-2"><label className="text-sm font-bold">闲鱼原价（元，可留空）<input type="number" min="0" step="0.01" className="mt-2 w-full rounded-xl border p-3 font-normal" value={draft.original_price_cent>0?money(draft.original_price_cent):''} onChange={event=>setDraft({...draft,original_price_cent:Math.max(0,Math.round((Number(event.target.value)||0)*100))})}/><span className="mt-1 block text-xs font-normal text-slate-400">用于闲鱼划线价，必须不低于最低售价</span></label><label className="text-sm font-bold">售价策略<select className="mt-2 w-full rounded-xl border p-3 font-normal" value={draft.price_strategy.mode} onChange={event=>setDraft({...draft,price_strategy:{...draft.price_strategy,mode:event.target.value as typeof draft.price_strategy.mode}})}><option value="manual">手工售价</option><option value="fixed_add">采购价 + 固定金额</option><option value="percent_add">采购价 + 百分比</option><option value="margin">目标毛利率</option></select></label>{draft.price_strategy.mode!=='manual'&&<label className="text-sm font-bold">策略值<input type="number" step="0.01" className="mt-2 w-full rounded-xl border p-3 font-normal" value={draft.price_strategy.value} onChange={event=>setDraft({...draft,price_strategy:{...draft.price_strategy,value:Number(event.target.value)||0}})}/></label>}<label className="text-sm font-bold">最低利润（元）<input type="number" min="0" step="0.01" className="mt-2 w-full rounded-xl border p-3 font-normal" value={money(draft.price_strategy.minimum_profit_cent)} onChange={event=>setDraft({...draft,price_strategy:{...draft.price_strategy,minimum_profit_cent:Math.max(0,Math.round((Number(event.target.value)||0)*100))}})}/></label><label className="text-sm font-bold">库存策略<select className="mt-2 w-full rounded-xl border p-3 font-normal" value={draft.stock_strategy.mode} onChange={event=>setDraft({...draft,stock_strategy:{...draft.stock_strategy,mode:event.target.value as typeof draft.stock_strategy.mode}})}><option value="manual">手工库存</option><option value="mirror">跟随拼多多</option><option value="cap">跟随并设置上限</option><option value="fixed">固定安全库存</option></select></label>{draft.stock_strategy.mode==='cap'&&<label className="text-sm font-bold">库存上限<input type="number" min="0" className="mt-2 w-full rounded-xl border p-3 font-normal" value={draft.stock_strategy.cap} onChange={event=>setDraft({...draft,stock_strategy:{...draft.stock_strategy,cap:Math.max(0,Number(event.target.value)||0)}})}/></label>}{draft.stock_strategy.mode==='fixed'&&<label className="text-sm font-bold">固定库存<input type="number" min="0" className="mt-2 w-full rounded-xl border p-3 font-normal" value={draft.stock_strategy.fixed_quantity} onChange={event=>setDraft({...draft,stock_strategy:{...draft.stock_strategy,fixed_quantity:Math.max(0,Number(event.target.value)||0)}})}/></label>}<label className="flex items-center gap-2 text-sm font-bold"><input type="checkbox" checked={draft.stock_strategy.disable_when_oos} onChange={event=>setDraft({...draft,stock_strategy:{...draft.stock_strategy,disable_when_oos:event.target.checked}})}/>来源缺货时停用 SKU</label></div>{draft.source_properties.length>0&&<div><div className="text-sm font-bold">采集商品属性</div><div className="mt-2 flex flex-wrap gap-2">{draft.source_properties.map(property=><span key={property.name} className="rounded-lg bg-slate-100 px-3 py-1.5 text-xs"><b>{property.name}</b>：{property.values.join('、')}</span>)}</div></div>}</section>
		{draft.source_type === 'pdd' && <section className="rounded-2xl border bg-white p-5 shadow-sm"><div><h3 className="font-black">组合采集商品</h3><p className="mt-1 text-xs text-slate-500">输入已采集的拼多多商品 ID，将它的 SKU 合并到当前素材；每个 SKU 保留独立的 goods_id + sku_id 映射。</p></div><div className="mt-4 flex gap-2"><input className="min-w-0 flex-1 rounded-xl border p-3" placeholder="拼多多 goods_id" value={combineGoodsID} onChange={event=>setCombineGoodsID(event.target.value)}/><button type="button" disabled={combining} className="rounded-xl bg-brand px-4 font-bold text-white disabled:opacity-50" onClick={()=>void combinePDDProduct()}>{combining?'读取中…':'合并商品'}</button></div><div className="mt-3 flex flex-wrap gap-2">{sourceGoodsIDs.map(goodsID=><span key={goodsID} className="flex items-center gap-2 rounded-full bg-slate-100 px-3 py-1.5 text-xs"><span className="font-mono">{goodsID}</span>{sourceGoodsIDs.length>1&&<button type="button" title="从素材移除此来源及其 SKU" className="text-red-500" onClick={()=>removePDDSource(goodsID)}><X className="h-3 w-3"/></button>}</span>)}</div></section>}
	        <section className="rounded-2xl border bg-white p-5 shadow-sm"><div className="mb-4 flex justify-between"><h3 className="font-black">商品规格</h3><span className="text-xs text-slate-400">最多添加 2 个规格类型</span></div>
          <div className="space-y-3">{specifications.map((specification, specIndex) => <div key={specIndex} className="rounded-xl bg-slate-50 p-4"><div className="flex flex-wrap items-center gap-3"><select className="rounded-lg border bg-white p-2" value={['颜色','尺码','容量','数量','款式'].includes(specification.name) ? specification.name : '__custom'} onChange={event => { const name = event.target.value === '__custom' ? '' : event.target.value; applySpecifications(specifications.map((item, index) => index === specIndex ? { ...item, name } : item)); }}><option value="">请选择规格类型</option><option>颜色</option><option>尺码</option><option>容量</option><option>数量</option><option>款式</option><option value="__custom">自定义</option></select>{(!['颜色','尺码','容量','数量','款式'].includes(specification.name)) && <input className="w-32 rounded-lg border p-2" placeholder="规格名称" value={specification.name} onChange={event => applySpecifications(specifications.map((item, index) => index === specIndex ? { ...item, name: event.target.value } : item))}/>}<label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={specification.supportImage} onChange={event => setImageSpecification(specIndex, event.target.checked)}/>支持添加图片</label><button className="ml-auto text-red-500" onClick={() => removeSpecification(specIndex)}><Trash2 className="h-4 w-4"/></button></div><div className="mt-3 flex flex-wrap gap-2">{specification.values.map((value, valueIndex) => <div key={valueIndex} className="flex items-center gap-1 rounded-lg border bg-white p-1"><input className="w-28 p-1.5" value={value.value} onChange={event => applySpecifications(specifications.map((item, index) => index === specIndex ? { ...item, values: item.values.map((entry, row) => row === valueIndex ? { ...entry, value: event.target.value } : entry) } : item))}/>{specification.supportImage && <label className="flex h-8 w-8 cursor-pointer items-center justify-center overflow-hidden rounded border">{value.image_url ? <img src={value.image_url} className="h-full w-full object-cover"/> : <ImagePlus className="h-4 w-4"/>}<input type="file" accept="image/*" className="hidden" onChange={async event => { const file = event.target.files?.[0]; if (!file) return; const result = await uploadMaterialImage(file); setPropertyImage(specification.name, value.value, result.url); }}/></label>}<button className="p-1 text-slate-400 hover:text-red-500" onClick={() => applySpecifications(specifications.map((item, index) => index === specIndex ? { ...item, values: item.values.filter((_, row) => row !== valueIndex) } : item))}><X className="h-3 w-3"/></button></div>)}<button className="rounded-lg border border-dashed px-3 py-2 text-sm text-slate-500" onClick={() => applySpecifications(specifications.map((item, index) => index === specIndex ? { ...item, values: [...item.values, { value: '' }] } : item))}>+ 输入规格值</button></div></div>)}{specifications.length < 2 && <button className="rounded-xl bg-slate-50 px-5 py-3 font-bold text-slate-600" onClick={() => applySpecifications([...specifications, { name: '', supportImage: false, values: [{ value: '' }] }])}><Plus className="mr-1 inline h-4 w-4"/>添加规格类型 ({specifications.length}/2)</button>}</div>
          <div className="my-4 flex justify-between"><h4 className="font-bold">SKU 价格与库存</h4><span className="text-xs text-slate-400">{draft.skus.length} 个组合</span></div>
		  <div className="mb-4 flex flex-wrap gap-3 rounded-xl border bg-slate-50 p-3"><div className="flex items-center gap-2"><span className="text-sm font-bold">闲鱼统一调价</span><input type="number" step="0.01" className="w-28 rounded-lg border bg-white p-2" placeholder="如 5 或 -5" value={priceAdjustment} onChange={event => setPriceAdjustment(event.target.value)}/><button type="button" className="rounded-lg bg-brand px-3 py-2 text-sm font-bold text-white" onClick={adjustAllPrices}>应用</button></div><div className="flex items-center gap-2"><span className="text-sm font-bold">统一库存</span><input type="number" min="0" step="1" className="w-28 rounded-lg border bg-white p-2" placeholder="如 100" value={batchStock} onChange={event => setBatchStock(event.target.value)}/><button type="button" className="rounded-lg border bg-white px-3 py-2 text-sm font-bold" onClick={setAllStock}>应用</button></div><p className="w-full text-xs text-slate-500">调价只修改闲鱼售价，不修改拼多多采购价。预计毛利 = 闲鱼售价 − 拼多多采购价（暂未扣除平台费用和运费）。</p></div>
          <div className="overflow-x-auto"><table className="w-full min-w-[980px] text-sm"><thead><tr>{dimensions.map(name => <th className="p-2 text-left" key={name}>{name}</th>)}{draft.source_type === 'pdd' && <><th className="p-2 text-left">拼多多采购价</th><th className="p-2 text-left">拼多多原价</th></>}<th className="p-2 text-left">闲鱼售价（元）</th>{draft.source_type === 'pdd' && <th className="p-2 text-left">预计毛利</th>}<th className="p-2 text-left">库存</th><th>操作</th></tr></thead>
            <tbody>{draft.skus.map((sku, index) => <tr className="border-t" key={sku.material_sku_id || `${sku.source_goods_id}-${sku.source_sku_id}` || index}>{dimensions.map(name => { const property = sku.properties.find(item => item.name === name); return <td className="p-2" key={name}><input className="w-28 rounded-lg border p-2" value={property?.value || ''} onChange={event => patchSKU(index, { properties: sku.properties.map(item => item.name === name ? { ...item, value: event.target.value } : item) })}/></td>; })}{draft.source_type === 'pdd' && <><td className="p-2 font-medium">{sku.source_price_cent ? `¥${money(sku.source_price_cent)}` : '-'}</td><td className="p-2 text-slate-500">{sku.source_normal_price_cent ? `¥${money(sku.source_normal_price_cent)}` : '-'}</td></>}<td className="p-2"><PriceInput cents={sku.price_cent} onChange={price_cent => patchSKU(index, { price_cent })}/></td>{draft.source_type === 'pdd' && <td className={`p-2 font-medium ${(grossProfit(sku) ?? 0) < 0 ? 'text-red-600' : 'text-emerald-600'}`}>{grossProfit(sku) === null ? '-' : <>{grossProfit(sku)! >= 0 ? '+' : '-'}¥{money(Math.abs(grossProfit(sku)!))}<span className="ml-1 text-xs text-slate-400">({grossMargin(sku)!.toFixed(1)}%)</span></>}</td>}<td className="p-2"><input type="number" min="0" className="w-24 rounded-lg border p-2" value={sku.quantity} onChange={event => patchSKU(index, { quantity: Number(event.target.value) })}/></td><td className="p-2 text-center"><button disabled={draft.skus.length <= 1} title="删除 SKU" className="text-red-500 disabled:opacity-30" onClick={() => setDraft({ ...draft, skus: draft.skus.filter((_, row) => row !== index) })}><Trash2 className="h-4 w-4"/></button></td></tr>)}</tbody></table></div>
          <p className="mt-3 text-xs text-slate-500">拼多多价格只由采集来源同步；每个组合可单独设置闲鱼售价和库存。</p>
          <button className="mt-4 flex items-center gap-2 rounded-xl border px-4 py-2 text-sm font-bold" onClick={() => { const properties = dimensions.map(name => ({ name, value: '' })); setDraft({ ...draft, skus: [...draft.skus, { sku_type: 'manual', price_cent: draft.skus[0]?.price_cent || 100, quantity: 1, enabled: true, properties }] }); }}><Plus className="h-4 w-4"/>添加手工 SKU</button>
		  <details className="mt-4 rounded-xl border bg-slate-50 p-3"><summary className="cursor-pointer font-bold">SKU 来源映射</summary><div className="mt-3 overflow-x-auto"><table className="w-full min-w-[1120px] text-xs"><thead><tr><th className="p-2 text-left">本地 SKU</th><th className="p-2 text-left">类型</th><th className="p-2 text-left">拼多多商品</th><th className="p-2 text-left">拼多多 SKU</th><th className="p-2 text-left">原始规格</th><th className="p-2 text-left">发布规格</th><th className="p-2 text-left">来源图片</th><th className="p-2 text-left">来源操作</th></tr></thead><tbody>{draft.skus.map((sku,index)=><tr className="border-t" key={sku.material_sku_id||`${sku.source_goods_id}-${sku.source_sku_id}`||index}><td className="p-2 font-mono">{sku.material_sku_id||'保存后生成'}</td><td className="p-2 font-bold">{sku.sku_type==='source'?'来源':sku.sku_type==='placeholder'?'占位':'手工'}</td><td className="p-2 font-mono">{sku.source_goods_id||(sku.source_sku_id?draft.source_id:'-')}</td><td className="p-2 font-mono">{sku.source_sku_id||'-'}</td><td className="p-2">{(sku.source_properties||[]).map(p=>`${p.name}=${p.value}`).join(' / ')||'-'}</td><td className="p-2">{sku.properties.map(p=>`${p.name}=${p.value}`).join(' / ')}</td><td className="p-2">{sku.source_image_url?<img src={sku.source_image_url} className="h-10 w-10 rounded object-cover"/>:'-'}</td><td className="p-2"><div className="flex flex-wrap gap-1"><button disabled={busy||!sku.material_sku_id} className="rounded border bg-white px-2 py-1 font-bold" onClick={()=>void changeSKUSource(index,'bind')}>绑定/更换</button><button disabled={busy||!sku.material_sku_id} className="rounded border bg-white px-2 py-1" onClick={()=>void changeSKUSource(index,'convert_manual')}>转手工</button><button disabled={busy||!sku.material_sku_id} className="rounded border bg-white px-2 py-1" onClick={()=>void changeSKUSource(index,'convert_placeholder')}>转占位</button></div></td></tr>)}</tbody></table></div></details>
        </section>
        <section className="rounded-2xl border bg-white p-5 shadow-sm"><h3 className="mb-4 font-black">发货设置</h3><div className="grid gap-4 sm:grid-cols-2"><label className="text-sm font-bold">运费方式<select className="mt-2 w-full rounded-xl border p-3 font-normal" value={draft.postage_mode} onChange={event => setDraft({ ...draft, postage_mode: event.target.value })}><option value="free">包邮</option><option value="distance">按距离计费</option><option value="fixed">固定邮费</option><option value="none">无需邮寄</option></select></label>{draft.postage_mode === 'fixed' && <label className="text-sm font-bold">邮费（元）<input type="number" min="0.01" step="0.01" className="mt-2 w-full rounded-xl border p-3 font-normal" value={money(draft.postage_cent)} onChange={event => setDraft({ ...draft, postage_cent: Math.round(Number(event.target.value) * 100) })}/></label>}</div></section>
		{draft.source_type === 'pdd' && <section className="rounded-2xl border bg-white p-5 shadow-sm"><div className="flex items-center justify-between"><div><h3 className="font-black">采集来源更新</h3><p className="text-xs text-slate-500">检查并同步全部 {sourceGoodsIDs.length} 个来源商品；不会覆盖发布规格文字和闲鱼售价。</p></div><button className="rounded-lg border px-3 py-2 text-sm font-bold" onClick={async()=>setSourceDiff(await getMaterialSourceDiff(draft.id))}>检查差异</button></div>{sourceDiff&&<div className="mt-3 rounded-xl bg-slate-50 p-3 text-sm"><p>新增 {sourceDiff.added.length} · 变化 {sourceDiff.changed.length} · 下架 {sourceDiff.removed.length}</p><div className="mt-3 flex flex-wrap gap-2"><button className="rounded-lg bg-brand px-3 py-2 font-bold text-white" onClick={async()=>{await syncMaterialSource(draft.id,{prices:true,stock:true,images:true,add_new:true,disable_removed:true});await onSaved();setError('拼多多价格、库存、图片及新 SKU 已同步；闲鱼售价未改动，请关闭后重新打开素材查看');}}>同步来源价格、库存、图片及新 SKU</button><button className="rounded-lg border bg-white px-3 py-2 font-bold" onClick={async()=>{await syncMaterialSource(draft.id,{prices:false,stock:true,images:false,add_new:false,disable_removed:true});await onSaved();setError('全部来源库存与下架状态已同步，请关闭后重新打开素材查看');}}>仅同步库存/下架</button></div></div>}</section>}
      </div>
      <aside className="space-y-5">
        <section className="rounded-2xl border bg-white p-5 shadow-sm"><div className="mb-4 flex justify-between"><h3 className="font-black">商品图片</h3><span className="text-xs text-slate-400">{draft.images.length}/9</span></div><div className="grid grid-cols-3 gap-2">{draft.images.map((url, index) => <div className="group relative aspect-square" key={`${url}-${index}`}><img src={url} referrerPolicy="no-referrer" className="h-full w-full rounded-xl object-cover"/>{index === 0 && <span className="absolute bottom-1 left-1 rounded bg-brand px-1.5 py-0.5 text-[10px] text-white">主图</span>}<div className="absolute inset-x-1 top-1 hidden flex-wrap gap-1 group-hover:flex"><button className="rounded bg-brand px-1.5 py-1 text-[10px] text-white" onClick={()=>setDraft({...draft,images:[url,...draft.images.filter((_,row)=>row!==index)]})}>设为主图</button><button className="rounded bg-black/60 p-1 text-white" onClick={()=>setMediaPreview({type:'image',url})}>预览</button><button disabled={index === 0} className="rounded bg-black/60 p-1 text-white disabled:opacity-30" onClick={() => { const images = [...draft.images]; [images[index - 1], images[index]] = [images[index], images[index - 1]]; setDraft({ ...draft, images }); }}><ArrowUp className="h-3 w-3"/></button><button disabled={index === draft.images.length - 1} className="rounded bg-black/60 p-1 text-white disabled:opacity-30" onClick={() => { const images = [...draft.images]; [images[index + 1], images[index]] = [images[index], images[index + 1]]; setDraft({ ...draft, images }); }}><ArrowDown className="h-3 w-3"/></button><button className="rounded bg-red-500 p-1 text-white" onClick={() => setDraft({ ...draft, images: draft.images.filter((_, row) => row !== index) })}><Trash2 className="h-3 w-3"/></button></div></div>)}</div><div className="mt-3 flex flex-wrap gap-2">{productImageChoices.length>0&&<button className="rounded-lg border px-3 py-2 text-xs font-bold" onClick={()=>setMediaPicker({kind:'product-image',selected:new Set()})}>商品采集图 {productImageChoices.length}</button>}{reviewImageChoices.length>0&&<button className="rounded-lg border px-3 py-2 text-xs font-bold" onClick={()=>setMediaPicker({kind:'review-image',selected:new Set()})}>评论图片 {reviewImageChoices.length}</button>}{draft.images.length < 9 && <label className="cursor-pointer rounded-lg border px-3 py-2 text-xs font-bold">本地上传<input type="file" accept="image/*" multiple className="hidden" onChange={async event => { const files = Array.from(event.target.files || []).slice(0, 9 - draft.images.length); const urls = await Promise.all(files.map(async file => (await uploadMaterialImage(file)).url)); setDraft(current => ({ ...current, images: [...current.images, ...urls] })); }}/></label>}</div></section>
        <section className="rounded-2xl border bg-white p-5 shadow-sm"><div className="flex items-center justify-between"><h3 className="font-black">视频</h3><label className="flex items-center gap-2 text-xs"><input type="checkbox" checked={draft.video_enabled!==false} onChange={e=>setDraft({...draft,video_enabled:e.target.checked})}/>发布视频（默认开启）</label></div><div className="mt-3 grid grid-cols-2 gap-2">{(draft.videos||[]).map((video,index)=><div key={`${video.url}-${index}`} className="relative overflow-hidden rounded-xl border bg-black"><video src={video.url} poster={video.cover_url} controls preload="metadata" className="aspect-video w-full"/><button className="absolute right-1 top-1 rounded bg-red-500 p-1 text-white" onClick={()=>setDraft({...draft,videos:draft.videos.filter((_,row)=>row!==index)})}><Trash2 className="h-3 w-3"/></button></div>)}</div>{reviewVideoChoices.length>0&&<button className="mt-3 rounded-lg border px-3 py-2 text-xs font-bold" onClick={()=>setMediaPicker({kind:'review-video',selected:new Set()})}><Play className="mr-1 inline h-3 w-3"/>评论视频 {reviewVideoChoices.length}</button>}<p className="mt-2 text-xs text-amber-600">视频可保存多个；当前闲鱼发布协议尚未接入，启用且已选视频时会阻止发布并明确提示。</p></section>
        {mode === 'publish' && <section className="rounded-2xl border bg-white p-5 shadow-sm space-y-4"><label className="text-sm font-bold">发布账号<select className="mt-2 w-full rounded-xl border p-3 font-normal" value={cookieID} onChange={event => {setCookieID(event.target.value);setPublishLocation(null);}}><option value="">请选择账号</option>{accounts.map(account => <option key={account.id} value={account.id}>{account.nickname || account.remark || account.id}{account.enabled ? '' : '（未启用）'}</option>)}</select></label><PublishLocationPicker accountID={cookieID} value={publishLocation} onChange={setPublishLocation} compact/></section>}
		<section className="rounded-2xl border bg-white p-5 shadow-sm"><h3 className="font-black">发布记录</h3>{publishRecords.length===0?<p className="mt-2 text-xs text-slate-500">暂无发布记录</p>:<div className="mt-3 space-y-2">{publishRecords.slice(0,5).map(record=><div key={record.id} className="rounded-lg bg-slate-50 p-2 text-xs"><b>{record.status==='success'?'成功':'失败'}</b> · {record.cookie_id}<br/>{record.published_item_id&&<>闲鱼商品：{record.published_item_id}<br/></>}{record.mapping_counts&&<>SKU 映射：成功 {record.mapping_counts.mapped||0} / 待处理 {record.mapping_counts.pending||0} / 未匹配 {record.mapping_counts.unmapped||0} / 冲突 {record.mapping_counts.ambiguous||0}<br/></>}{new Date(record.created_at*1000).toLocaleString()}</div>)}</div>}</section>
        {error && <div className="rounded-xl border border-red-200 bg-red-50 p-3 text-sm font-bold text-red-600">{error}</div>}
        <section className="rounded-2xl border bg-white p-5 shadow-sm"><div className="space-y-3"><button disabled={busy} onClick={() => void save()} className="flex w-full items-center justify-center gap-2 rounded-xl border p-3 font-black disabled:opacity-50"><Save className="h-4 w-4"/>保存素材</button>{mode === 'publish' && <button disabled={busy} onClick={() => void publish()} className="flex w-full items-center justify-center gap-2 rounded-xl bg-brand p-3 font-black text-white disabled:opacity-50"><Send className="h-4 w-4"/>{busy ? '正在发布…' : '保存并发布'}</button>}</div></section>
      </aside>
    </main>
    {mediaPicker&&<div className="fixed inset-0 z-[60] flex items-center justify-center bg-black/60 p-6"><div className="max-h-[85vh] w-full max-w-5xl overflow-auto rounded-2xl bg-white p-5"><div className="flex items-center justify-between"><h3 className="text-lg font-black">{mediaPicker.kind==='product-image'?'选择商品采集图':mediaPicker.kind==='review-image'?'选择评论图片':'选择评论视频'}</h3><button onClick={()=>setMediaPicker(null)}><X/></button></div><div className="mt-4 flex flex-wrap gap-2">{pickerGoodsIDs.length>1&&<select className="rounded-lg border px-3 py-2 text-sm" value={mediaGoodsFilter} onChange={event=>{setMediaGoodsFilter(event.target.value);setMediaSKUFilter('');}}><option value="">全部商品</option>{pickerGoodsIDs.map(id=><option key={id}>{id}</option>)}</select>}{mediaPicker.kind!=='product-image'&&<><select className="rounded-lg border px-3 py-2 text-sm" value={mediaSKUFilter} onChange={event=>setMediaSKUFilter(event.target.value)}><option value="">全部 SKU</option>{pickerSKUIds.map(id=><option key={id}>{id}</option>)}</select><select className="rounded-lg border px-3 py-2 text-sm" value={mediaSourceFilter} onChange={event=>setMediaSourceFilter(event.target.value)}><option value="">普通评价和追评</option><option value="initial">普通评价</option><option value="additional">追评</option></select></>}<span className="self-center text-xs text-slate-500">显示 {pickerChoices.length} / {unfilteredPickerChoices.length}，已选 {mediaPicker.selected.size}</span></div><div className="mt-4 grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">{pickerChoices.map(item=><article key={item.key} className={`rounded-xl border-2 p-2 ${mediaPicker.selected.has(item.key)?'border-brand':'border-slate-200'}`}><button className="block w-full" onClick={()=>setMediaPreview({key:item.key,type:item.type,url:item.url,cover:'cover' in item?item.cover:''})}>{item.type==='video'?<video src={item.url} poster={'cover' in item?item.cover:''} preload="metadata" className="aspect-square w-full rounded-lg bg-black object-contain"/>:<img src={item.url} referrerPolicy="no-referrer" className="aspect-square w-full rounded-lg object-cover"/>}</button><button className="mt-2 w-full rounded-lg border px-2 py-1 text-xs font-bold" onClick={()=>toggleMediaSelection(item.key)}>{mediaPicker.selected.has(item.key)?'已选择':'选择'}</button><p className="mt-1 truncate text-[10px] text-slate-500">{item.label}</p></article>)}</div><div className="sticky bottom-0 mt-4 flex justify-end gap-2 bg-white py-2"><button className="rounded-lg border px-4 py-2" onClick={()=>setMediaPicker(null)}>取消</button><button className="rounded-lg bg-brand px-4 py-2 font-bold text-white" onClick={confirmMediaPicker}>加入所选媒体（{mediaPicker.selected.size}）</button></div></div></div>}
    {mediaPreview&&<div role="dialog" aria-modal="true" aria-label="媒体预览" className="fixed inset-0 z-[70] flex items-center justify-center bg-black/90 p-6" onClick={()=>setMediaPreview(null)}>{mediaPreview.key&&pickerChoices.length>1&&<><button type="button" aria-label="上一项" className="absolute left-4 z-10 rounded-full bg-white/15 p-3 text-white hover:bg-white/25" onClick={event=>{event.stopPropagation();moveMediaPreview(-1);}}><ArrowLeft/></button><button type="button" aria-label="下一项" className="absolute right-4 z-10 rounded-full bg-white/15 p-3 text-white hover:bg-white/25" onClick={event=>{event.stopPropagation();moveMediaPreview(1);}}><ArrowRight/></button></>}{mediaPreview.type==='video'?<video key={mediaPreview.url} src={mediaPreview.url} poster={mediaPreview.cover} controls autoPlay className="max-h-[82vh] max-w-full" onClick={e=>e.stopPropagation()}/>:<img src={mediaPreview.url} referrerPolicy="no-referrer" className="max-h-[82vh] max-w-full object-contain" onClick={e=>e.stopPropagation()}/>}<button aria-label="关闭预览" className="absolute right-6 top-6 text-white" onClick={()=>setMediaPreview(null)}><X/></button>{mediaPreview.key&&<div className="absolute bottom-5 left-1/2 flex -translate-x-1/2 items-center gap-3 rounded-full bg-black/70 px-4 py-2 text-sm text-white" onClick={event=>event.stopPropagation()}><span>{Math.max(0,pickerChoices.findIndex(item=>item.key===mediaPreview.key))+1} / {pickerChoices.length}</span><button type="button" className={`rounded-full px-3 py-1 font-bold ${mediaPicker?.selected.has(mediaPreview.key)?'bg-brand':'bg-white/20'}`} onClick={()=>toggleMediaSelection(mediaPreview.key!)}>{mediaPicker?.selected.has(mediaPreview.key)?'已选择':'选择'}</button><span className="hidden text-xs text-white/60 sm:inline">← → 切换 · Enter/空格选择 · Esc 关闭</span></div>}</div>}
  </div>;
}

function MaterialSplitDialog({ material, onClose, onSaved }: { material:ProductMaterial;onClose:()=>void;onSaved:()=>Promise<void> }) {
  const specificationNames=useMemo(()=>Array.from(new Set(material.skus.flatMap(sku=>sku.properties.map(property=>property.name.trim())).filter(Boolean))),[material.skus]);
  const occupied=useMemo(()=>new Set(material.split_occupied_sku_ids||[]),[material.split_occupied_sku_ids]);
  const [selectedSpecification,setSelectedSpecification]=useState(specificationNames[0]||'');
  const [selectedValues,setSelectedValues]=useState<string[]>([]);
  const [shortcutMode,setShortcutMode]=useState<'separate'|'merge'>('separate');
  const [groups,setGroups]=useState([{id:'manual-1',name:'分组 1',title:`${material.title} - 分组 1`},{id:'manual-2',name:'分组 2',title:`${material.title} - 分组 2`}]);
  const [assignments,setAssignments]=useState<Record<string,string>>({});
  const [filter,setFilter]=useState('');
  const [bulkGroup,setBulkGroup]=useState('manual-1');
  const [busy,setBusy]=useState(false);
  const [error,setError]=useState('');
  const [idempotencyKey]=useState(()=>globalThis.crypto?.randomUUID?.()||`${Date.now()}-${Math.random()}`);
  const allSpecificationValues=useMemo(()=>Array.from(new Set(material.skus.map(sku=>sku.properties.find(property=>property.name.trim()===selectedSpecification)?.value.trim()).filter((value):value is string=>!!value))),[material.skus,selectedSpecification]);
  const specificationValues=useMemo(()=>{
    const values=new Map<string,{total:number;available:number}>();
    for(const sku of material.skus){
      const value=sku.properties.find(property=>property.name.trim()===selectedSpecification)?.value.trim();
      if(!value)continue;
      const row=values.get(value)||{total:0,available:0};row.total++;if(sku.material_sku_id&&!occupied.has(sku.material_sku_id)&&!assignments[sku.material_sku_id])row.available++;values.set(value,row);
    }
    return Array.from(values.entries()).map(([value,counts])=>({value,...counts}));
  },[material.skus,occupied,selectedSpecification,assignments]);
  useEffect(()=>setSelectedValues(allSpecificationValues),[allSpecificationValues]);
  const rows=material.skus.filter(sku=>`${sku.source_sku_id||''} ${sku.properties.map(p=>`${p.name}=${p.value}`).join(' ')}`.toLowerCase().includes(filter.trim().toLowerCase()));
  const count=(groupID:string)=>Object.values(assignments).filter(value=>value===groupID).length;
  const assignedTotal=Object.keys(assignments).length;
  const missingStableID=material.skus.some(sku=>!sku.material_sku_id);
  const generateShortcut=(replace=false)=>{
    setError('');
    if(!selectedSpecification||selectedValues.length===0)return setError('请选择至少一个仍可拆分的规格值');
    let generated;
    const unavailable=new Set(occupied);if(!replace)for(const stableID of Object.keys(assignments))unavailable.add(stableID);
    try { generated=buildShortcutSplitPlans({materialTitle:material.title,skus:material.skus,occupiedIDs:unavailable,specification:selectedSpecification,selectedValues,mode:shortcutMode}); }
    catch(reason){return setError(reason instanceof Error?reason.message:'生成拆分预览失败');}
    const {candidateCount,plans}=generated;
    if(candidateCount===0)return setError('所选规格值没有可拆分的 SKU');
    const preview=applyShortcutPlans({existingGroups:groups,existingAssignments:assignments,plans,replace,idPrefix:`shortcut-${idempotencyKey}`});
    if(preview.groups.length>20)return setError(`追加后会有 ${preview.groups.length} 个子素材，单次最多 20 个；请减少勾选值或使用“清空并重新生成”`);
    setGroups(preview.groups);setAssignments(preview.assignments);setBulkGroup(preview.groups[preview.groups.length-plans.length]?.id||preview.groups[0]?.id||'');
  };
  const submit=async()=>{
    setError('');
    const payload=groups.map(group=>({name:group.name.trim(),title:group.title.trim(),material_sku_ids:material.skus.filter(sku=>sku.material_sku_id&&assignments[sku.material_sku_id]===group.id).map(sku=>sku.material_sku_id!)})).filter(group=>group.material_sku_ids.length>0);
    if(payload.length===0)return setError('请至少给一个分组分配 SKU');
    if(groups.some(group=>!group.name.trim()))return setError('分组名称不能为空');
    if(new Set(groups.map(group=>group.name.trim())).size!==groups.length)return setError('分组名称不能重复');
    if(payload.some(group=>group.material_sku_ids.length>MATERIAL_SKU_GROUP_LIMIT))return setError(`每个子素材最多 ${MATERIAL_SKU_GROUP_LIMIT} 个 SKU`);
    setBusy(true);
    try { const result=await splitMaterial(material.id,payload,material.split_version||'',idempotencyKey); await onSaved(); alert(`已创建 ${result.children.length} 个子素材，分配 ${result.selected_count} 个 SKU，未分配 ${result.remaining_count} 个`); onClose(); }
    catch (reason) { setError(reason instanceof Error?reason.message:'拆分失败'); }
    finally { setBusy(false); }
  };
  return <div className="fixed inset-0 z-50 overflow-auto bg-slate-100 p-4 sm:p-8"><div className="mx-auto max-w-6xl space-y-4">
    <header className="flex items-start justify-between rounded-2xl border bg-white p-5"><div><h2 className="text-xl font-black">手动拆分素材</h2><p className="mt-1 text-sm text-slate-500">只创建本地子素材；不会修改采集商品、发布记录或平台商品。SKU 来源映射会原样保留。</p></div><button onClick={onClose}><X/></button></header>
    <section className="rounded-2xl border bg-white p-5"><h3 className="font-black">按规格快捷生成分组</h3><p className="mt-1 text-xs text-slate-500">先选择一个发布规格，再勾选规格值。这里只生成预览，仍可在下方逐个调整 SKU。</p><div className="mt-4 flex flex-wrap gap-2">{specificationNames.map(name=><button key={name} className={`rounded-lg border px-3 py-2 text-sm font-bold ${selectedSpecification===name?'border-brand bg-brand text-white':'bg-white'}`} onClick={()=>setSelectedSpecification(name)}>{name}</button>)}</div>
      {selectedSpecification&&<div className="mt-4 grid gap-2 sm:grid-cols-2 lg:grid-cols-3">{specificationValues.map(row=><label key={row.value} className={`flex items-center justify-between rounded-lg border p-3 text-sm ${row.available===0?'bg-slate-50 text-slate-400':''}`}><span className="flex items-center gap-2"><input type="checkbox" disabled={row.available===0} checked={selectedValues.includes(row.value)} onChange={event=>setSelectedValues(current=>event.target.checked?[...current,row.value]:current.filter(value=>value!==row.value))}/><b>{row.value}</b></span><span>{row.available}/{row.total} 可拆</span></label>)}</div>}
      <div className="mt-4 flex flex-wrap items-center gap-3"><label className="flex items-center gap-2 text-sm"><input type="radio" checked={shortcutMode==='separate'} onChange={()=>setShortcutMode('separate')}/>每个规格值单独成组</label><label className="flex items-center gap-2 text-sm"><input type="radio" checked={shortcutMode==='merge'} onChange={()=>setShortcutMode('merge')}/>勾选值合并成组</label><button className="rounded-lg bg-indigo-600 px-4 py-2 text-sm font-bold text-white" onClick={()=>generateShortcut(false)}>{assignedTotal>0?'追加到拆分预览':'生成拆分预览'}</button>{assignedTotal>0&&<><button className="rounded-lg border px-4 py-2 text-sm font-bold" onClick={()=>generateShortcut(true)}>覆盖当前预览</button><button className="rounded-lg border border-red-200 px-4 py-2 text-sm font-bold text-red-600" onClick={()=>{const cleared=clearSplitPreview();setGroups(cleared.groups);setAssignments(cleared.assignments);setBulkGroup('');setError('');}}>清空分组</button></>}<span className="text-xs text-slate-500">超过 {MATERIAL_SKU_GROUP_LIMIT} SKU 自动分段；单次最多 20 组</span></div>
    </section>
    <section className="rounded-2xl border bg-white p-5">{assignedTotal>0&&<div className="mb-4 rounded-xl bg-indigo-50 p-3 text-sm font-black text-indigo-700">拆分预览：已分配 {assignedTotal} SKU → {groups.filter(group=>count(group.id)>0).map(group=>count(group.id)).join(' + ')}，共 {groups.filter(group=>count(group.id)>0).length} 组</div>}<div className="flex flex-wrap items-center gap-2"><input className="min-w-64 flex-1 rounded-lg border px-3 py-2" placeholder="筛选规格或拼多多 SKU" value={filter} onChange={event=>setFilter(event.target.value)}/><select className="min-w-0 max-w-full rounded-lg border px-3 py-2" value={bulkGroup} onChange={event=>setBulkGroup(event.target.value)}>{groups.map(group=><option key={group.id} value={group.id}>{group.name}</option>)}</select><button className="rounded-lg border px-3 py-2 font-bold" onClick={()=>setAssignments(current=>{const next={...current};rows.forEach(sku=>{if(sku.material_sku_id&&!occupied.has(sku.material_sku_id))next[sku.material_sku_id]=bulkGroup;});return next;})}>将当前筛选项分配到该组</button></div>
      <div className="mt-4 space-y-3">{groups.map((group,index)=><div key={group.id} className="rounded-xl border p-3"><div className="flex gap-2"><input className="w-40 rounded-lg border p-2 font-bold" value={group.name} onChange={event=>{const next=[...groups];next[index]={...group,name:event.target.value};setGroups(next);}}/><input className="min-w-0 flex-1 rounded-lg border p-2" value={group.title} onChange={event=>{const next=[...groups];next[index]={...group,title:event.target.value};setGroups(next);}}/><button onClick={()=>{const removed=removeSplitPreviewGroup({groups,assignments,groupID:group.id});setGroups(removed.groups);setAssignments(removed.assignments);if(bulkGroup===group.id)setBulkGroup(removed.groups[0]?.id||'');}}><Trash2 className="h-4 w-4 text-red-500"/></button></div><div className="mt-2 flex items-center justify-between"><p className={`text-sm font-black ${count(group.id)>MATERIAL_SKU_GROUP_LIMIT?'text-red-600':'text-slate-700'}`}>{count(group.id)} / {MATERIAL_SKU_GROUP_LIMIT} SKU</p><span className="text-xs text-slate-400">第 {index+1} 组，共 {groups.length} 组</span></div></div>)}</div>
      <button disabled={groups.length>=20} className="mt-3 flex items-center gap-1 rounded-lg border px-3 py-2 text-sm font-bold" onClick={()=>{const name=`分组 ${groups.length+1}`,id=`manual-${Date.now()}-${groups.length+1}`;setGroups([...groups,{id,name,title:`${material.title} - ${name}`}]);setBulkGroup(id);}}><Plus className="h-4 w-4"/>添加分组</button>
    </section>
    <section className="overflow-hidden rounded-2xl border bg-white"><div className="max-h-[52vh] overflow-auto"><table className="w-full min-w-[760px] text-sm"><thead className="sticky top-0 bg-slate-50"><tr><th className="p-3 text-left">发布规格</th><th className="p-3 text-left">拼多多 SKU</th><th className="p-3 text-left">类型</th><th className="p-3 text-left">分配到</th></tr></thead><tbody>{rows.map((sku,index)=>{const isOccupied=!!sku.material_sku_id&&occupied.has(sku.material_sku_id);return <tr className="border-t" key={sku.material_sku_id||index}><td className="p-3">{sku.properties.map(p=>`${p.name}=${p.value}`).join(' / ')||'-'}</td><td className="p-3 font-mono">{sku.source_sku_id||'-'}</td><td className="p-3">{isOccupied?'已被子素材占用':sku.sku_type==='source'?'来源':sku.sku_type==='placeholder'?'占位':'手工'}</td><td className="p-3"><select className="rounded-lg border px-3 py-2" value={sku.material_sku_id?assignments[sku.material_sku_id]||'':''} disabled={!sku.material_sku_id||isOccupied} onChange={event=>sku.material_sku_id&&setAssignments({...assignments,[sku.material_sku_id]:event.target.value})}><option value="">{isOccupied?'不可重复分配':'未分配'}</option>{groups.map(group=><option key={group.id} value={group.id}>{group.name}</option>)}</select></td></tr>})}</tbody></table></div></section>
    {missingStableID&&<p className="rounded-xl bg-amber-50 p-3 text-sm font-bold text-amber-700">存在未生成稳定 ID 的 SKU，请先进入编辑页保存一次。</p>}{error&&<p className="rounded-xl bg-red-50 p-3 text-sm font-bold text-red-600">{error}</p>}
    <footer className="flex justify-end gap-3 rounded-2xl border bg-white p-4"><button className="rounded-xl border px-5 py-2" onClick={onClose}>取消</button><button disabled={busy||missingStableID} className="rounded-xl bg-brand px-5 py-2 font-bold text-white disabled:opacity-50" onClick={()=>void submit()}>{busy?'正在拆分…':'创建子素材'}</button></footer>
  </div></div>;
}

const ProductMaterials: React.FC = () => {
  const [items, setItems] = useState<ProductMaterial[]>([]);
  const [accounts, setAccounts] = useState<AccountDetail[]>([]);
  const [editor, setEditor] = useState<{ material: ProductMaterial; mode: EditorMode } | null>(null);
  const [splitTarget,setSplitTarget]=useState<ProductMaterial|null>(null);
  const [search, setSearch] = useState('');
  const [activeSearch, setActiveSearch] = useState('');
  const load = async () => setItems(await getMaterials(activeSearch));
  useEffect(() => { void load(); void getAccountDetails().then(setAccounts); }, []);
  return <div className="space-y-4"><div><h2 className="text-2xl font-black">发布素材库</h2><p className="text-sm text-slate-500">素材和发布使用同一个商品编辑器；发布时选择账号，不会修改采集源数据。</p></div>
    <form className="flex max-w-xl gap-2" onSubmit={async event => { event.preventDefault(); const query=search.trim(); setActiveSearch(query); setItems(await getMaterials(query)); }}><div className="relative flex-1"><Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400"/><input className="w-full rounded-xl border bg-white py-2.5 pl-10 pr-3" placeholder="搜索素材标题或拼多多商品 ID" value={search} onChange={event=>setSearch(event.target.value)}/></div><button className="rounded-xl bg-brand px-5 font-bold text-white">搜索</button>{activeSearch&&<button type="button" className="rounded-xl border bg-white px-4 font-bold" onClick={async()=>{setSearch('');setActiveSearch('');setItems(await getMaterials());}}>清除</button>}</form>
    {items.length === 0 ? <div className="rounded-2xl border bg-white p-10 text-center text-slate-500">{activeSearch?'没有匹配的素材':'暂无素材，请先从采集商品创建素材'}</div> : <div className="grid gap-4 lg:grid-cols-2">{items.map(material => { const sourcePrices = material.skus.map(item=>item.source_price_cent||0).filter(price=>price>0); const salePrices = material.skus.map(item=>item.price_cent); return <article key={material.id} className="rounded-2xl border bg-white p-5 shadow-sm"><div className="flex gap-4">{material.images[0] ? <img src={material.images[0]} referrerPolicy="no-referrer" className="h-24 w-24 rounded-xl object-cover"/> : <div className="h-24 w-24 rounded-xl bg-slate-100"/>}<div className="min-w-0 flex-1"><h3 className="truncate font-black">{material.title}</h3><p className="mt-1 text-xs text-slate-400">来源：{material.source_type === 'pdd' ? '拼多多' : '手工'} · {material.skus.length} SKU · {material.images.length} 图</p>{(material.parent_material_id||0)>0&&<p className="mt-1 text-xs font-bold text-indigo-600">拆分组：{material.split_group_name}</p>}{material.is_split_source&&<p className="mt-1 text-xs font-bold text-amber-600">已生成拆分子素材；原素材仍可用于发布测试</p>}{material.source_type==='pdd'&&<p className="mt-1 text-xs text-slate-500">拼多多商品 ID：<span className="font-mono">{material.source_id}</span></p>}<p className="mt-2 text-sm text-slate-600">闲鱼 ¥{money(Math.min(...salePrices))}{Math.min(...salePrices)!==Math.max(...salePrices)&&`–${money(Math.max(...salePrices))}`}</p>{sourcePrices.length>0&&<p className="text-xs text-slate-400">拼多多 ¥{money(Math.min(...sourcePrices))}{Math.min(...sourcePrices)!==Math.max(...sourcePrices)&&`–${money(Math.max(...sourcePrices))}`}</p>}</div></div><div className="mt-4 flex flex-wrap gap-3"><button onClick={() => setEditor({ material, mode: 'edit' })} className="flex items-center gap-1 font-bold text-brand"><PackagePlus className="h-4 w-4"/>编辑</button><button onClick={() => setEditor({ material, mode: 'publish' })} className="flex items-center gap-1 font-bold text-emerald-600"><Send className="h-4 w-4"/>发布</button>{!material.parent_material_id&&<button onClick={()=>setSplitTarget(material)} className="flex items-center gap-1 font-bold text-indigo-600"><Scissors className="h-4 w-4"/>拆分</button>}<button onClick={() => setEditor({ material: { ...clone(material), id: material.id }, mode: 'edit' })} className="hidden items-center gap-1 font-bold text-slate-500"><Copy className="h-4 w-4"/>复制</button><button onClick={async () => { if (confirm('删除此素材？不会影响采集商品和闲鱼平台商品。')) { await deleteMaterial(material.id); await load(); } }} className="ml-auto flex items-center gap-1 font-bold text-red-500"><Trash2 className="h-4 w-4"/>删除</button></div></article>; })}</div>}
    {editor && <ProductEditor initial={editor.material} mode={editor.mode} accounts={accounts} onClose={() => setEditor(null)} onSaved={load}/>} {splitTarget&&<MaterialSplitDialog material={splitTarget} onClose={()=>setSplitTarget(null)} onSaved={load}/>}</div>;
};

export default ProductMaterials;
