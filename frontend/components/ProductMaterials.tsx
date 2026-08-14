import React, { useEffect, useMemo, useState } from 'react';
import { ArrowDown, ArrowUp, Copy, ImagePlus, PackagePlus, Plus, Save, Search, Send, Trash2, X } from 'lucide-react';
import {
  deleteMaterial, getAccountDetails, getMaterials, getMaterialPublishRecords, getMaterialSourceDiff, MaterialPublishRecord, ProductMaterial, ProductMaterialSKU,
  publishMaterial, syncMaterialSource, updateMaterial, uploadMaterialImage,
} from '../services/api';
import type { AccountDetail } from '../types';

type EditorMode = 'edit' | 'publish';
type Specification = { name: string; supportImage: boolean; values: Array<{ value: string; image_url?: string }> };

const clone = <T,>(value: T): T => structuredClone(value);
const money = (cents: number) => (cents / 100).toFixed(2);
const deriveSpecifications = (skus: ProductMaterialSKU[]): Specification[] => {
  const result: Specification[] = [];
  for (const sku of skus) for (const property of sku.properties) {
    let specification = result.find(item => item.name === property.name);
    if (!specification) { specification = { name: property.name, supportImage: false, values: [] }; result.push(specification); }
    if (!specification.values.some(item => item.value === property.value)) specification.values.push({ value: property.value, image_url: property.image_url });
    if (property.image_url) specification.supportImage = true;
  }
	const imageSpecificationIndex = result.findLastIndex(specification => specification.supportImage);
	for (const [index, specification] of result.entries()) if (specification.supportImage && index !== imageSpecificationIndex) {
		specification.supportImage = false;
		specification.values = specification.values.map(value => ({ ...value, image_url: undefined }));
	}
  return result.slice(0, 2);
};
const skuKey = (properties: Array<{ name: string; value: string }>) => properties.map(item => `${item.name}=${item.value}`).join('\0');
const generateSKUs = (specifications: Specification[], previous: ProductMaterialSKU[]): ProductMaterialSKU[] => {
  if (!specifications.length || specifications.some(item => !item.name.trim() || !item.values.length)) return previous;
  const combinations = specifications.reduce<Array<Array<{ name: string; value: string; image_url?: string }>>>((rows, specification) => rows.flatMap(row => specification.values.filter(item => item.value.trim()).map(item => [...row, { name: specification.name, value: item.value, image_url: specification.supportImage ? item.image_url : undefined }])), [[]]);
  const previousByKey = new Map(previous.map(item => [skuKey(item.properties), item]));
  const sameShape = combinations.length === previous.length && previous.every(item => item.properties.length === specifications.length);
  return combinations.slice(0, 200).map((properties, index) => {
    const matched = previousByKey.get(skuKey(properties)) || (sameShape ? previous[index] : undefined);
    return matched ? { ...matched, properties } : { price_cent: previous[0]?.price_cent || 100, quantity: previous[0]?.quantity ?? 1, enabled: true, properties };
  });
};

function ProductEditor({ initial, mode, accounts, onClose, onSaved }: {
  initial: ProductMaterial; mode: EditorMode; accounts: AccountDetail[];
  onClose: () => void; onSaved: () => Promise<void>;
}) {
  const [draft, setDraft] = useState(() => clone(initial));
  const [cookieID, setCookieID] = useState(accounts.find(account => account.enabled)?.id || '');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
	const [priceAdjustment, setPriceAdjustment] = useState('');
  const [batchStock, setBatchStock] = useState('');
	const [publishRecords, setPublishRecords] = useState<MaterialPublishRecord[]>([]);
	const [sourceDiff, setSourceDiff] = useState<{added:any[];changed:any[];removed:string[]}|null>(null);
  const [specifications, setSpecifications] = useState<Specification[]>(() => deriveSpecifications(initial.skus).map(item => ({ ...item, supportImage: item.name === initial.image_property_name, values: item.values.map(value => ({ ...value, image_url: item.name === initial.image_property_name ? value.image_url : undefined })) })));
  const dimensions = useMemo(() => specifications.map(item => item.name), [specifications]);
	useEffect(() => {
		const imageName = specifications.find(item => item.supportImage)?.name || '';
		setDraft(current => ({ ...current, skus: current.skus.map(sku => ({ ...sku, properties: sku.properties.map(property => property.name === imageName ? property : { ...property, image_url: undefined }) })) }));
	}, []);
	useEffect(() => { void getMaterialPublishRecords(initial.id).then(setPublishRecords); }, [initial.id]);

  const applySpecifications = (next: Specification[]) => { setSpecifications(next); setDraft(current => ({ ...current, skus: generateSKUs(next, current.skus) })); };
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
		setDraft(current => ({ ...current, skus: current.skus.map(sku => ({ ...sku, quantity })) }));
		setError('');
	};
  const setPropertyImage = (name: string, value: string, imageURL: string) => {
    setSpecifications(current => current.map(specification => specification.name === name ? { ...specification, values: specification.values.map(item => item.value === value ? { ...item, image_url: imageURL } : item) } : specification));
    setDraft(current => ({ ...current, skus: current.skus.map(sku => ({ ...sku, properties: sku.properties.map(property => property.name === name && property.value === value ? { ...property, image_url: imageURL } : property) })) }));
  };
  const validate = () => {
    if (!draft.title.trim()) return '请填写商品标题';
    if (!draft.description.trim()) return '请填写商品描述';
    if (draft.images.length < 1 || draft.images.length > 9) return '商品图片必须为 1 到 9 张';
    if (draft.skus.length < 1 || draft.skus.length > 200) return 'SKU 必须为 1 到 200 行';
    if (draft.skus.some(sku => sku.price_cent <= 0 || sku.quantity < 0 || !sku.properties.length)) return '请完善所有 SKU 的规格、价格和库存';
    const keys = draft.skus.map(sku => sku.properties.map(property => `${property.name}=${property.value}`).join('\0'));
    if (new Set(keys).size !== keys.length) return '存在重复的 SKU 规格组合';
    return '';
  };
  const save = async () => {
    const message = validate(); if (message) return setError(message);
    setBusy(true); setError('');
    try {
      await updateMaterial(draft.id, {
        title: draft.title, description: draft.description, images: draft.images,
        category: draft.category, skus: draft.skus, postage_mode: draft.postage_mode,
        postage_cent: draft.postage_cent,
		image_property_name: draft.image_property_name || '',
      });
      await onSaved();
      if (mode === 'edit') onClose();
    } catch (reason: any) { setError(reason?.message || '保存失败'); }
    finally { setBusy(false); }
  };
  const publish = async () => {
    const message = validate(); if (message) return setError(message);
    if (!cookieID) return setError('请选择发布账号');
    setBusy(true); setError('');
    try {
      await updateMaterial(draft.id, {
        title: draft.title, description: draft.description, images: draft.images,
        category: draft.category, skus: draft.skus, postage_mode: draft.postage_mode,
        postage_cent: draft.postage_cent,
		image_property_name: draft.image_property_name || '',
      });
      const result = await publishMaterial(draft.id, cookieID);
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
        </section>
        <section className="rounded-2xl border bg-white p-5 shadow-sm"><div className="mb-4 flex justify-between"><h3 className="font-black">商品规格</h3><span className="text-xs text-slate-400">最多添加 2 个规格类型</span></div>
          <div className="space-y-3">{specifications.map((specification, specIndex) => <div key={specIndex} className="rounded-xl bg-slate-50 p-4"><div className="flex flex-wrap items-center gap-3"><select className="rounded-lg border bg-white p-2" value={['颜色','尺码','容量','数量','款式'].includes(specification.name) ? specification.name : '__custom'} onChange={event => { const name = event.target.value === '__custom' ? '' : event.target.value; applySpecifications(specifications.map((item, index) => index === specIndex ? { ...item, name } : item)); }}><option value="">请选择规格类型</option><option>颜色</option><option>尺码</option><option>容量</option><option>数量</option><option>款式</option><option value="__custom">自定义</option></select>{(!['颜色','尺码','容量','数量','款式'].includes(specification.name)) && <input className="w-32 rounded-lg border p-2" placeholder="规格名称" value={specification.name} onChange={event => applySpecifications(specifications.map((item, index) => index === specIndex ? { ...item, name: event.target.value } : item))}/>}<label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={specification.supportImage} onChange={event => setImageSpecification(specIndex, event.target.checked)}/>支持添加图片</label><button className="ml-auto text-red-500" onClick={() => applySpecifications(specifications.filter((_, index) => index !== specIndex))}><Trash2 className="h-4 w-4"/></button></div><div className="mt-3 flex flex-wrap gap-2">{specification.values.map((value, valueIndex) => <div key={valueIndex} className="flex items-center gap-1 rounded-lg border bg-white p-1"><input className="w-28 p-1.5" value={value.value} onChange={event => applySpecifications(specifications.map((item, index) => index === specIndex ? { ...item, values: item.values.map((entry, row) => row === valueIndex ? { ...entry, value: event.target.value } : entry) } : item))}/>{specification.supportImage && <label className="flex h-8 w-8 cursor-pointer items-center justify-center overflow-hidden rounded border">{value.image_url ? <img src={value.image_url} className="h-full w-full object-cover"/> : <ImagePlus className="h-4 w-4"/>}<input type="file" accept="image/*" className="hidden" onChange={async event => { const file = event.target.files?.[0]; if (!file) return; const result = await uploadMaterialImage(file); setPropertyImage(specification.name, value.value, result.url); }}/></label>}<button className="p-1 text-slate-400 hover:text-red-500" onClick={() => applySpecifications(specifications.map((item, index) => index === specIndex ? { ...item, values: item.values.filter((_, row) => row !== valueIndex) } : item))}><X className="h-3 w-3"/></button></div>)}<button className="rounded-lg border border-dashed px-3 py-2 text-sm text-slate-500" onClick={() => applySpecifications(specifications.map((item, index) => index === specIndex ? { ...item, values: [...item.values, { value: '' }] } : item))}>+ 输入规格值</button></div></div>)}{specifications.length < 2 && <button className="rounded-xl bg-slate-50 px-5 py-3 font-bold text-slate-600" onClick={() => applySpecifications([...specifications, { name: '', supportImage: false, values: [{ value: '' }] }])}><Plus className="mr-1 inline h-4 w-4"/>添加规格类型 ({specifications.length}/2)</button>}</div>
          <div className="my-4 flex justify-between"><h4 className="font-bold">SKU 价格与库存</h4><span className="text-xs text-slate-400">{draft.skus.length} 个组合</span></div>
		  <div className="mb-4 flex flex-wrap gap-3 rounded-xl border bg-slate-50 p-3"><div className="flex items-center gap-2"><span className="text-sm font-bold">统一调价</span><input type="number" step="0.01" className="w-28 rounded-lg border bg-white p-2" placeholder="如 5 或 -5" value={priceAdjustment} onChange={event => setPriceAdjustment(event.target.value)}/><button type="button" className="rounded-lg bg-brand px-3 py-2 text-sm font-bold text-white" onClick={adjustAllPrices}>应用</button></div><div className="flex items-center gap-2"><span className="text-sm font-bold">统一库存</span><input type="number" min="0" step="1" className="w-28 rounded-lg border bg-white p-2" placeholder="如 100" value={batchStock} onChange={event => setBatchStock(event.target.value)}/><button type="button" className="rounded-lg border bg-white px-3 py-2 text-sm font-bold" onClick={setAllStock}>应用</button></div><p className="w-full text-xs text-slate-500">统一调价是在现有售价上加减：输入 5 表示 ¥10 → ¥15，输入 -5 表示 ¥10 → ¥5。</p></div>
          <div className="overflow-x-auto"><table className="w-full min-w-[720px] text-sm"><thead><tr>{dimensions.map(name => <th className="p-2 text-left" key={name}>{name}</th>)}<th className="p-2 text-left">售价（元）</th><th className="p-2 text-left">库存</th><th>操作</th></tr></thead>
            <tbody>{draft.skus.map((sku, index) => <tr className="border-t" key={sku.source_sku_id || index}>{dimensions.map(name => { const property = sku.properties.find(item => item.name === name); return <td className="p-2" key={name}><input className="w-28 rounded-lg border p-2" value={property?.value || ''} onChange={event => patchSKU(index, { properties: sku.properties.map(item => item.name === name ? { ...item, value: event.target.value } : item) })}/></td>; })}<td className="p-2"><input type="number" min="0.01" step="0.01" className="w-28 rounded-lg border p-2" value={money(sku.price_cent)} onChange={event => patchSKU(index, { price_cent: Math.round(Number(event.target.value) * 100) })}/></td><td className="p-2"><input type="number" min="0" className="w-24 rounded-lg border p-2" value={sku.quantity} onChange={event => patchSKU(index, { quantity: Number(event.target.value) })}/></td><td className="p-2 text-center"><button disabled={draft.skus.length <= 1} title="删除 SKU" className="text-red-500 disabled:opacity-30" onClick={() => setDraft({ ...draft, skus: draft.skus.filter((_, row) => row !== index) })}><Trash2 className="h-4 w-4"/></button></td></tr>)}</tbody></table></div>
          <p className="mt-3 text-xs text-slate-500">每个组合可单独设置价格和库存；规格图片只在上方一个规格类型中维护。</p>
          <button className="mt-4 flex items-center gap-2 rounded-xl border px-4 py-2 text-sm font-bold" onClick={() => { const properties = dimensions.map(name => ({ name, value: '' })); setDraft({ ...draft, skus: [...draft.skus, { price_cent: draft.skus[0]?.price_cent || 100, quantity: 1, enabled: true, properties }] }); }}><Plus className="h-4 w-4"/>添加 SKU</button>
		  <details className="mt-4 rounded-xl border bg-slate-50 p-3"><summary className="cursor-pointer font-bold">SKU 来源映射</summary><div className="mt-3 overflow-x-auto"><table className="w-full min-w-[760px] text-xs"><thead><tr><th className="p-2 text-left">本地 SKU</th><th className="p-2 text-left">拼多多 SKU</th><th className="p-2 text-left">原始规格</th><th className="p-2 text-left">发布规格</th><th className="p-2 text-left">来源图片</th></tr></thead><tbody>{draft.skus.map((sku,index)=><tr className="border-t" key={sku.material_sku_id||index}><td className="p-2 font-mono">{sku.material_sku_id||'保存后生成'}</td><td className="p-2 font-mono">{sku.source_sku_id||'手工'}</td><td className="p-2">{(sku.source_properties||[]).map(p=>`${p.name}=${p.value}`).join(' / ')||'-'}</td><td className="p-2">{sku.properties.map(p=>`${p.name}=${p.value}`).join(' / ')}</td><td className="p-2">{sku.source_image_url?<img src={sku.source_image_url} className="h-10 w-10 rounded object-cover"/>:'-'}</td></tr>)}</tbody></table></div></details>
        </section>
        <section className="rounded-2xl border bg-white p-5 shadow-sm"><h3 className="mb-4 font-black">发货设置</h3><div className="grid gap-4 sm:grid-cols-2"><label className="text-sm font-bold">运费方式<select className="mt-2 w-full rounded-xl border p-3 font-normal" value={draft.postage_mode} onChange={event => setDraft({ ...draft, postage_mode: event.target.value })}><option value="free">包邮</option><option value="fixed">固定邮费</option></select></label>{draft.postage_mode === 'fixed' && <label className="text-sm font-bold">邮费（元）<input type="number" min="0.01" step="0.01" className="mt-2 w-full rounded-xl border p-3 font-normal" value={money(draft.postage_cent)} onChange={event => setDraft({ ...draft, postage_cent: Math.round(Number(event.target.value) * 100) })}/></label>}</div></section>
		{draft.source_type === 'pdd' && <section className="rounded-2xl border bg-white p-5 shadow-sm"><div className="flex items-center justify-between"><div><h3 className="font-black">采集来源更新</h3><p className="text-xs text-slate-500">先检查差异，再选择同步内容；不会覆盖发布规格文字。</p></div><button className="rounded-lg border px-3 py-2 text-sm font-bold" onClick={async()=>setSourceDiff(await getMaterialSourceDiff(draft.id))}>检查差异</button></div>{sourceDiff&&<div className="mt-3 rounded-xl bg-slate-50 p-3 text-sm"><p>新增 {sourceDiff.added.length} · 变化 {sourceDiff.changed.length} · 下架 {sourceDiff.removed.length}</p><div className="mt-3 flex flex-wrap gap-2"><button className="rounded-lg bg-brand px-3 py-2 font-bold text-white" onClick={async()=>{await syncMaterialSource(draft.id,{prices:true,stock:true,images:true,add_new:true,disable_removed:true});await onSaved();setError('来源数据已同步，请关闭后重新打开素材查看');}}>同步价格、库存、图片及新 SKU</button><button className="rounded-lg border bg-white px-3 py-2 font-bold" onClick={async()=>{await syncMaterialSource(draft.id,{prices:false,stock:true,images:false,add_new:false,disable_removed:true});await onSaved();setError('库存与下架状态已同步，请关闭后重新打开素材查看');}}>仅同步库存/下架</button></div></div>}</section>}
      </div>
      <aside className="space-y-5">
        <section className="rounded-2xl border bg-white p-5 shadow-sm"><div className="mb-4 flex justify-between"><h3 className="font-black">商品图片</h3><span className="text-xs text-slate-400">{draft.images.length}/9</span></div><div className="grid grid-cols-3 gap-2">{draft.images.map((url, index) => <div className="group relative aspect-square" key={`${url}-${index}`}><img src={url} referrerPolicy="no-referrer" className="h-full w-full rounded-xl object-cover"/>{index === 0 && <span className="absolute bottom-1 left-1 rounded bg-brand px-1.5 py-0.5 text-[10px] text-white">主图</span>}<div className="absolute right-1 top-1 hidden gap-1 group-hover:flex"><button disabled={index === 0} className="rounded bg-black/60 p-1 text-white disabled:opacity-30" onClick={() => { const images = [...draft.images]; [images[index - 1], images[index]] = [images[index], images[index - 1]]; setDraft({ ...draft, images }); }}><ArrowUp className="h-3 w-3"/></button><button disabled={index === draft.images.length - 1} className="rounded bg-black/60 p-1 text-white disabled:opacity-30" onClick={() => { const images = [...draft.images]; [images[index + 1], images[index]] = [images[index], images[index + 1]]; setDraft({ ...draft, images }); }}><ArrowDown className="h-3 w-3"/></button><button className="rounded bg-red-500 p-1 text-white" onClick={() => setDraft({ ...draft, images: draft.images.filter((_, row) => row !== index) })}><Trash2 className="h-3 w-3"/></button></div></div>)}{draft.images.length < 9 && <label className="flex aspect-square cursor-pointer flex-col items-center justify-center rounded-xl border-2 border-dashed text-xs font-bold text-slate-500"><ImagePlus className="mb-1 h-5 w-5"/>上传<input type="file" accept="image/*" multiple className="hidden" onChange={async event => { const files = Array.from(event.target.files || []).slice(0, 9 - draft.images.length); const urls = await Promise.all(files.map(async file => (await uploadMaterialImage(file)).url)); setDraft(current => ({ ...current, images: [...current.images, ...urls] })); }}/></label>}</div></section>
        {mode === 'publish' && <section className="rounded-2xl border bg-white p-5 shadow-sm"><label className="text-sm font-bold">发布账号<select className="mt-2 w-full rounded-xl border p-3 font-normal" value={cookieID} onChange={event => setCookieID(event.target.value)}><option value="">请选择账号</option>{accounts.map(account => <option key={account.id} value={account.id}>{account.nickname || account.remark || account.id}{account.enabled ? '' : '（未启用）'}</option>)}</select></label></section>}
		<section className="rounded-2xl border bg-white p-5 shadow-sm"><h3 className="font-black">发布记录</h3>{publishRecords.length===0?<p className="mt-2 text-xs text-slate-500">暂无发布记录</p>:<div className="mt-3 space-y-2">{publishRecords.slice(0,5).map(record=><div key={record.id} className="rounded-lg bg-slate-50 p-2 text-xs"><b>{record.status==='success'?'成功':'失败'}</b> · {record.cookie_id}<br/>{record.published_item_id&&<>闲鱼商品：{record.published_item_id}<br/></>}{new Date(record.created_at*1000).toLocaleString()}</div>)}</div>}</section>
        {error && <div className="rounded-xl border border-red-200 bg-red-50 p-3 text-sm font-bold text-red-600">{error}</div>}
        <section className="rounded-2xl border bg-white p-5 shadow-sm"><div className="space-y-3"><button disabled={busy} onClick={() => void save()} className="flex w-full items-center justify-center gap-2 rounded-xl border p-3 font-black disabled:opacity-50"><Save className="h-4 w-4"/>保存素材</button>{mode === 'publish' && <button disabled={busy} onClick={() => void publish()} className="flex w-full items-center justify-center gap-2 rounded-xl bg-brand p-3 font-black text-white disabled:opacity-50"><Send className="h-4 w-4"/>{busy ? '正在发布…' : '保存并发布'}</button>}</div></section>
      </aside>
    </main>
  </div>;
}

const ProductMaterials: React.FC = () => {
  const [items, setItems] = useState<ProductMaterial[]>([]);
  const [accounts, setAccounts] = useState<AccountDetail[]>([]);
  const [editor, setEditor] = useState<{ material: ProductMaterial; mode: EditorMode } | null>(null);
  const [search, setSearch] = useState('');
  const [activeSearch, setActiveSearch] = useState('');
  const load = async () => setItems(await getMaterials(activeSearch));
  useEffect(() => { void load(); void getAccountDetails().then(setAccounts); }, []);
  return <div className="space-y-4"><div><h2 className="text-2xl font-black">发布素材库</h2><p className="text-sm text-slate-500">素材和发布使用同一个商品编辑器；发布时选择账号，不会修改采集源数据。</p></div>
    <form className="flex max-w-xl gap-2" onSubmit={async event => { event.preventDefault(); const query=search.trim(); setActiveSearch(query); setItems(await getMaterials(query)); }}><div className="relative flex-1"><Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400"/><input className="w-full rounded-xl border bg-white py-2.5 pl-10 pr-3" placeholder="搜索素材标题或拼多多商品 ID" value={search} onChange={event=>setSearch(event.target.value)}/></div><button className="rounded-xl bg-brand px-5 font-bold text-white">搜索</button>{activeSearch&&<button type="button" className="rounded-xl border bg-white px-4 font-bold" onClick={async()=>{setSearch('');setActiveSearch('');setItems(await getMaterials());}}>清除</button>}</form>
    {items.length === 0 ? <div className="rounded-2xl border bg-white p-10 text-center text-slate-500">{activeSearch?'没有匹配的素材':'暂无素材，请先从采集商品创建素材'}</div> : <div className="grid gap-4 lg:grid-cols-2">{items.map(material => <article key={material.id} className="rounded-2xl border bg-white p-5 shadow-sm"><div className="flex gap-4">{material.images[0] ? <img src={material.images[0]} referrerPolicy="no-referrer" className="h-24 w-24 rounded-xl object-cover"/> : <div className="h-24 w-24 rounded-xl bg-slate-100"/>}<div className="min-w-0 flex-1"><h3 className="truncate font-black">{material.title}</h3><p className="mt-1 text-xs text-slate-400">来源：{material.source_type === 'pdd' ? '拼多多' : '手工'} · {material.skus.length} SKU · {material.images.length} 图</p>{material.source_type==='pdd'&&<p className="mt-1 text-xs text-slate-500">拼多多商品 ID：<span className="font-mono">{material.source_id}</span></p>}<p className="mt-2 text-sm text-slate-500">¥{money(Math.min(...material.skus.map(item => item.price_cent)))}</p></div></div><div className="mt-4 flex flex-wrap gap-3"><button onClick={() => setEditor({ material, mode: 'edit' })} className="flex items-center gap-1 font-bold text-brand"><PackagePlus className="h-4 w-4"/>编辑</button><button onClick={() => setEditor({ material, mode: 'publish' })} className="flex items-center gap-1 font-bold text-emerald-600"><Send className="h-4 w-4"/>发布</button><button onClick={() => setEditor({ material: { ...clone(material), id: material.id }, mode: 'edit' })} className="hidden items-center gap-1 font-bold text-slate-500"><Copy className="h-4 w-4"/>复制</button><button onClick={async () => { if (confirm('删除此素材？不会影响采集商品和闲鱼平台商品。')) { await deleteMaterial(material.id); await load(); } }} className="ml-auto flex items-center gap-1 font-bold text-red-500"><Trash2 className="h-4 w-4"/>删除</button></div></article>)}</div>}
    {editor && <ProductEditor initial={editor.material} mode={editor.mode} accounts={accounts} onClose={() => setEditor(null)} onSaved={load}/>}</div>;
};

export default ProductMaterials;
