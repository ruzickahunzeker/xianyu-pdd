import React, { useEffect, useState } from 'react';
import { ChevronDown, ChevronUp, ExternalLink, ImageOff, PackageSearch, RefreshCw } from 'lucide-react';
import { createMaterialFromPDD, deletePDDProduct, getPDDProduct, getPDDProducts, PDDProductDetail, PDDProductSummary, refreshPDDProduct } from '../services/api';

const money = (cent: number) => `¥${(cent / 100).toFixed(2)}`;
const formatTime = (value: number) => value ? new Date(value * 1000).toLocaleString() : '—';
const specText = (specs: PDDProductDetail['skus'][number]['specs']) => specs.map(spec => `${spec.spec_key || '规格'}：${spec.raw_value}`).join(' / ') || '无规格信息';

const PDDCollectedProducts: React.FC = () => {
  const [products, setProducts] = useState<PDDProductSummary[]>([]);
  const [details, setDetails] = useState<Record<string, PDDProductDetail>>({});
  const [opened, setOpened] = useState('');
  const [loading, setLoading] = useState(true);
  const [detailLoading, setDetailLoading] = useState('');
  const [error, setError] = useState('');
  const [creating, setCreating] = useState('');
  const [refreshing, setRefreshing] = useState('');

  const load = async () => {
    setLoading(true); setError('');
    try { setProducts(await getPDDProducts()); }
    catch (err) { setError(err instanceof Error ? err.message : '加载采集商品失败'); }
    finally { setLoading(false); }
  };
  useEffect(() => { void load(); }, []);

  const toggle = async (goodsId: string) => {
    if (opened === goodsId) { setOpened(''); return; }
    setOpened(goodsId);
    if (details[goodsId]) return;
    setDetailLoading(goodsId); setError('');
    try {
      const detail = await getPDDProduct(goodsId);
      setDetails(current => ({ ...current, [goodsId]: detail }));
    }
    catch (err) { setError(err instanceof Error ? err.message : '加载 SKU 失败'); setOpened(''); }
    finally { setDetailLoading(''); }
  };

  const refreshProduct = async (goodsId: string) => {
    setRefreshing(goodsId);
    try {
      const result = await refreshPDDProduct(goodsId);
      const detail = await getPDDProduct(goodsId);
      setDetails(current => ({ ...current, [goodsId]: detail }));
      await load();
      const missing = result.missing_suspected.length ? `\n疑似缺失 SKU（已保留）：${result.missing_suspected.join('、')}` : '';
      alert(`更新完成：读取 ${result.sku_count} 个 SKU，新增 ${result.added}，价格变化 ${result.price_changed}，库存变化 ${result.stock_changed}，状态变化 ${result.status_changed}；同步 ${result.material_stock_updates} 个素材 SKU 库存。${missing}`);
    } catch (err) {
      alert(err instanceof Error ? err.message : '更新商品与 SKU 失败');
    } finally {
      setRefreshing('');
    }
  };

  return <div className="space-y-5">
    <div className="flex items-center justify-between gap-4">
      <div><h2 className="text-2xl font-black text-slate-950">已采集商品</h2><p className="mt-1 text-sm text-slate-500">查看拼多多商品和完整 SKU 原始规格。</p></div>
      <button onClick={() => void load()} className="flex items-center gap-2 rounded-xl border border-slate-200 bg-white px-4 py-2 font-bold text-slate-600"><RefreshCw className="h-4 w-4" />刷新</button>
    </div>
    {error && <div className="rounded-xl bg-red-50 p-4 font-bold text-red-600">{error}</div>}
    {loading ? <div className="rounded-2xl border bg-white p-10 text-center text-slate-500">加载中…</div> : products.length === 0 ? <div className="rounded-2xl border bg-white p-12 text-center text-slate-500"><PackageSearch className="mx-auto mb-3 h-9 w-9" />尚未采集商品</div> : products.map(product => {
      const detail = details[product.goods_id];
      const image = product.images?.[0];
      return <section key={product.goods_id} className="overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm">
        <button onClick={() => void toggle(product.goods_id)} className="grid w-full gap-4 p-5 text-left md:grid-cols-[72px_minmax(0,1fr)_150px_150px_28px] md:items-center">
          {image ? <img src={image} className="h-[72px] w-[72px] rounded-xl object-cover" referrerPolicy="no-referrer" /> : <span className="flex h-[72px] w-[72px] items-center justify-center rounded-xl bg-slate-100 text-slate-400"><ImageOff /></span>}
          <div className="min-w-0"><div className="truncate font-black text-slate-900">{product.title || '（未采集到标题）'}</div><div className="mt-1 text-xs text-slate-400">商品 ID：{product.goods_id}</div><div className="mt-2 text-sm font-bold text-brand">{money(product.min_price_cent)}{product.max_price_cent !== product.min_price_cent ? ` - ${money(product.max_price_cent)}` : ''}</div></div>
          <div><div className="text-xs text-slate-400">SKU</div><div className="font-black">{product.sku_count} 个 <span className="text-sm text-emerald-600">({product.onsale_sku_count} 在售)</span></div></div>
          <div><div className="text-xs text-slate-400">最后采集</div><div className="text-sm font-bold">{formatTime(product.last_collected_at)}</div></div>
          {opened === product.goods_id ? <ChevronUp /> : <ChevronDown />}
        </button>
        {opened === product.goods_id && <div className="border-t border-slate-100 bg-slate-50 p-4 md:p-5">
          <div className="mb-4 flex flex-wrap justify-end gap-3"><button disabled={refreshing===product.goods_id} onClick={()=>void refreshProduct(product.goods_id)} className="flex items-center gap-2 rounded-xl border border-brand/30 bg-white px-4 py-2 text-sm font-black text-brand disabled:opacity-50"><RefreshCw className={`h-4 w-4 ${refreshing===product.goods_id?'animate-spin':''}`} />{refreshing===product.goods_id?'更新中…':'更新商品与 SKU'}</button><button disabled={creating===product.goods_id} onClick={async()=>{setCreating(product.goods_id);try{await createMaterialFromPDD(product.goods_id);alert('已创建素材草稿，请切换到素材库继续编辑')}catch(e){alert(e instanceof Error?e.message:'创建草稿失败')}finally{setCreating('')}}} className="rounded-xl bg-emerald-500 px-4 py-2 text-sm font-black text-white">{creating===product.goods_id?'创建中…':'创建发布草稿'}</button><button onClick={async()=>{if(!confirm('删除本地采集商品？关联素材草稿和闲鱼商品不会删除。'))return;try{const r=await deletePDDProduct(product.goods_id);alert(`${r.message}${r.draft_count?`，保留 ${r.draft_count} 个草稿`:''}`);setOpened('');await load()}catch(e){alert(e instanceof Error?e.message:'删除失败')}}} className="rounded-xl border border-red-200 px-4 py-2 text-sm font-black text-red-600">删除采集商品</button><a href={/^https:\/\/mobile\.(?:pinduoduo|yangkeduo)\.com\/goods\.html\?/.test(product.final_url||'')?product.final_url:`https://mobile.pinduoduo.com/goods.html?goods_id=${encodeURIComponent(product.goods_id)}`} target="_blank" rel="noreferrer" className="flex items-center gap-1 text-sm font-bold text-brand">打开拼多多商品 <ExternalLink className="h-4 w-4" /></a></div>
          {detailLoading === product.goods_id || !detail ? <div className="p-8 text-center text-slate-500">正在加载 SKU…</div> : <><div className="mb-4 rounded-xl border bg-white p-4"><h4 className="mb-2 text-sm font-black">商品属性</h4>{detail.goods_property?.length?<div className="flex flex-wrap gap-2">{detail.goods_property.map(property=><span key={property.key} className="rounded-lg bg-slate-100 px-3 py-1.5 text-xs"><b>{property.key}</b>：{property.values.join('、')}</span>)}</div>:<p className="text-xs text-slate-400">暂无属性；品牌和发货地不会采集。</p>}</div><div className="overflow-x-auto rounded-xl border border-slate-200 bg-white"><table className="min-w-[900px] w-full text-sm"><thead className="bg-slate-100 text-left text-xs text-slate-500"><tr><th className="px-4 py-3">SKU / 图片</th><th className="px-4 py-3">完整规格原值</th><th className="px-4 py-3">价格</th><th className="px-4 py-3">库存</th><th className="px-4 py-3">状态</th></tr></thead><tbody className="divide-y divide-slate-100">{detail.skus.map(sku => <tr key={sku.sku_id}><td className="px-4 py-3"><div className="flex items-center gap-3">{sku.thumb_url && <img src={sku.thumb_url} className="h-12 w-12 rounded-lg object-cover" referrerPolicy="no-referrer" />}<code className="text-xs">{sku.sku_id}</code></div></td><td className="max-w-xl px-4 py-3 font-bold text-slate-700">{specText(sku.specs)}</td><td className="px-4 py-3 font-black text-rose-600">{money(sku.price_cent)}</td><td className="px-4 py-3 font-bold">{sku.stock_exact ? sku.stock : `≥${sku.stock}（页面封顶）`}</td><td className="px-4 py-3"><span className={`rounded-full px-3 py-1 text-xs font-black ${sku.is_onsale ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-500'}`}>{sku.is_onsale ? '在售' : '停售'}</span></td></tr>)}</tbody></table></div></>}
        </div>}
      </section>;
    })}
  </div>;
};
export default PDDCollectedProducts;
