import React, { useEffect, useState } from 'react';
import { ChevronDown, ChevronUp, ExternalLink, ImageOff, PackageSearch, Play, RefreshCw, Send } from 'lucide-react';
import { createMaterialFromPDD, deletePDDProduct, getPDDProduct, getPDDProducts, PDDProductDetail, PDDProductSummary, refreshPDDProduct, syncPDDProductToRemote, testPDDRemoteCollector } from '../services/api';

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
  const [syncing, setSyncing] = useState('');
  const [syncTarget, setSyncTarget] = useState('');
  const [syncToken, setSyncToken] = useState('');
  const [testingTarget, setTestingTarget] = useState(false);

  const syncProduct = async (goodsId:string) => {
    if (!syncTarget.trim() || !syncToken.trim()) { alert('请先填写目标服务器地址和该服务器创建的采集设备 Token'); return; }
    setSyncing(goodsId);
    try {
      const result=await syncPDDProductToRemote(goodsId,syncTarget.trim(),syncToken.trim());
      alert(`同步成功：商品 ${result.goods_id}，${result.sku_count} 个 SKU`);
    } catch (err) { alert(err instanceof Error?err.message:'同步到目标服务器失败'); }
    finally { setSyncing(''); }
  };

  const syncAll = async () => {
    if (!syncTarget.trim() || !syncToken.trim()) { alert('请先填写目标服务器地址和采集设备 Token'); return; }
    if (!confirm(`确认将 ${products.length} 个采集商品依次同步到目标服务器？`)) return;
    let success=0; const failures:string[]=[];
    for (const product of products) {
      setSyncing(product.goods_id);
      try { await syncPDDProductToRemote(product.goods_id,syncTarget.trim(),syncToken.trim()); success++; }
      catch (err) { failures.push(`${product.goods_id}: ${err instanceof Error?err.message:'失败'}`); }
    }
    setSyncing('');
    alert(`批量同步完成：成功 ${success}，失败 ${failures.length}${failures.length?`\n${failures.slice(0,10).join('\n')}`:''}`);
  };

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
      const message = err instanceof Error ? err.message : '更新商品与 SKU 失败';
      alert(`${message}\n\n本次 Curl 更新未写入数据，原商品与 SKU 已保留。`);
    } finally {
      setRefreshing('');
    }
  };

  return <div className="space-y-5">
    <section className="rounded-2xl border border-sky-200 bg-sky-50 p-4">
      <div className="flex flex-wrap items-end gap-3">
        <label className="min-w-[260px] flex-1 text-sm font-bold text-slate-700">目标服务器地址<input value={syncTarget} onChange={event=>setSyncTarget(event.target.value)} placeholder="http://10.10.1.10:59188" className="mt-2 w-full rounded-xl border bg-white p-3 font-normal"/></label>
        <label className="min-w-[260px] flex-1 text-sm font-bold text-slate-700">目标服务器采集设备 Token<input type="password" value={syncToken} onChange={event=>setSyncToken(event.target.value)} autoComplete="off" placeholder="仅保留在当前页面，刷新后清空" className="mt-2 w-full rounded-xl border bg-white p-3 font-normal"/></label>
		<button disabled={testingTarget||!syncTarget.trim()||!syncToken.trim()} onClick={async()=>{setTestingTarget(true);try{await testPDDRemoteCollector(syncTarget.trim(),syncToken.trim());alert('目标服务器连接和 Token 验证成功')}catch(err){alert(err instanceof Error?err.message:'连接测试失败')}finally{setTestingTarget(false)}}} className="rounded-xl border border-sky-300 bg-white px-4 py-3 font-black text-sky-700 disabled:opacity-50">{testingTarget?'测试中…':'测试连接'}</button>
        <button disabled={!!syncing||products.length===0} onClick={()=>void syncAll()} className="flex items-center gap-2 rounded-xl bg-sky-600 px-4 py-3 font-black text-white disabled:opacity-50"><Send className="h-4 w-4"/>同步全部</button>
      </div>
      <p className="mt-3 text-xs text-sky-800">仅传商品、属性、图片/视频链接及 SKU 采购数据；不会传 Cookie、账号、素材、订单或履约数据。请在目标服务器“采集设备”中新建 Token。</p>
    </section>
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
          <div className="min-w-0"><div className="truncate font-black text-slate-900">{product.title || '（未采集到标题）'}</div><div className="mt-1 flex items-center gap-2 text-xs text-slate-400"><span>商品 ID：{product.goods_id}</span>{product.videos?.length>0&&<span className="inline-flex items-center gap-1 rounded bg-violet-50 px-2 py-0.5 font-bold text-violet-600"><Play className="h-3 w-3"/>视频 {product.videos.length}</span>}</div><div className="mt-2 text-sm font-bold text-brand">{money(product.min_price_cent)}{product.max_price_cent !== product.min_price_cent ? ` - ${money(product.max_price_cent)}` : ''}</div></div>
          <div><div className="text-xs text-slate-400">SKU</div><div className="font-black">{product.sku_count} 个 <span className="text-sm text-emerald-600">({product.onsale_sku_count} 在售)</span></div></div>
          <div><div className="text-xs text-slate-400">最后采集</div><div className="text-sm font-bold">{formatTime(product.last_collected_at)}</div></div>
          {opened === product.goods_id ? <ChevronUp /> : <ChevronDown />}
        </button>
        {opened === product.goods_id && <div className="border-t border-slate-100 bg-slate-50 p-4 md:p-5">
          <div className="mb-3 flex justify-end"><button disabled={!!syncing} onClick={()=>void syncProduct(product.goods_id)} className="flex items-center gap-2 rounded-xl border border-sky-300 bg-white px-4 py-2 text-sm font-black text-sky-700 disabled:opacity-50"><Send className="h-4 w-4"/>{syncing===product.goods_id?'同步中…':'同步到服务器'}</button></div>
          <div className="mb-4 flex flex-wrap justify-end gap-3"><button disabled={refreshing===product.goods_id} onClick={()=>void refreshProduct(product.goods_id)} className="flex items-center gap-2 rounded-xl border border-brand/30 bg-white px-4 py-2 text-sm font-black text-brand disabled:opacity-50"><RefreshCw className={`h-4 w-4 ${refreshing===product.goods_id?'animate-spin':''}`} />{refreshing===product.goods_id?'更新中…':'更新商品与 SKU'}</button><button disabled={creating===product.goods_id} onClick={async()=>{setCreating(product.goods_id);try{await createMaterialFromPDD(product.goods_id);alert('已创建素材草稿，请切换到素材库继续编辑')}catch(e){alert(e instanceof Error?e.message:'创建草稿失败')}finally{setCreating('')}}} className="rounded-xl bg-emerald-500 px-4 py-2 text-sm font-black text-white">{creating===product.goods_id?'创建中…':'创建发布草稿'}</button><button onClick={async()=>{if(!confirm('删除本地采集商品？关联素材草稿和闲鱼商品不会删除。'))return;try{const r=await deletePDDProduct(product.goods_id);alert(`${r.message}${r.draft_count?`，保留 ${r.draft_count} 个草稿`:''}`);setOpened('');await load()}catch(e){alert(e instanceof Error?e.message:'删除失败')}}} className="rounded-xl border border-red-200 px-4 py-2 text-sm font-black text-red-600">删除采集商品</button><a href={/^https:\/\/mobile\.(?:pinduoduo|yangkeduo)\.com\/goods\.html\?/.test(product.final_url||'')?product.final_url:`https://mobile.pinduoduo.com/goods.html?goods_id=${encodeURIComponent(product.goods_id)}`} target="_blank" rel="noreferrer" className="flex items-center gap-1 text-sm font-bold text-brand">数据库商品链接（扩展更新） <ExternalLink className="h-4 w-4" /></a></div>
          {detailLoading === product.goods_id || !detail ? <div className="p-8 text-center text-slate-500">正在加载 SKU…</div> : <>{detail.videos?.length>0&&<div className="mb-4 rounded-xl border bg-white p-4"><h4 className="mb-3 text-sm font-black">商品视频（创建草稿时自动加入）</h4><div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">{detail.videos.map(video=><video key={video.url} src={video.url} poster={video.cover_url} controls preload="metadata" className="aspect-video w-full rounded-xl bg-black object-contain"/>)}</div></div>}<div className="mb-4 rounded-xl border bg-white p-4"><h4 className="mb-2 text-sm font-black">商品属性</h4><div className="flex flex-wrap gap-2">{detail.goods_property?.map(property=><span key={property.key} className="rounded-lg bg-slate-100 px-3 py-1.5 text-xs"><b>{property.key}</b>：{property.values.join('、')}</span>)}</div>{!detail.goods_property?.length&&<p className="text-xs text-slate-400">暂无属性；品牌和发货地不会采集。</p>}</div><div className="overflow-x-auto rounded-xl border border-slate-200 bg-white"><table className="min-w-[900px] w-full text-sm"><thead className="bg-slate-100 text-left text-xs text-slate-500"><tr><th className="px-4 py-3">SKU / 图片</th><th className="px-4 py-3">完整规格原值</th><th className="px-4 py-3">价格</th><th className="px-4 py-3">库存</th><th className="px-4 py-3">状态</th></tr></thead><tbody className="divide-y divide-slate-100">{detail.skus.map(sku => <tr key={sku.sku_id}><td className="px-4 py-3"><div className="flex items-center gap-3">{sku.thumb_url && <img src={sku.thumb_url} className="h-12 w-12 rounded-lg object-cover" referrerPolicy="no-referrer" />}<code className="text-xs">{sku.sku_id}</code></div></td><td className="max-w-xl px-4 py-3 font-bold text-slate-700">{specText(sku.specs)}</td><td className="px-4 py-3 font-black text-rose-600">{money(sku.price_cent)}</td><td className="px-4 py-3 font-bold">{sku.stock_exact ? sku.stock : `≥${sku.stock}（页面封顶）`}</td><td className="px-4 py-3"><span className={`rounded-full px-3 py-1 text-xs font-black ${sku.is_onsale ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-500'}`}>{sku.is_onsale ? '在售' : '停售'}</span></td></tr>)}</tbody></table></div></>}
        </div>}
      </section>;
    })}
  </div>;
};
export default PDDCollectedProducts;
