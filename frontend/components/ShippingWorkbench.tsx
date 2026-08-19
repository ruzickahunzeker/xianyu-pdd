import React, { useEffect, useMemo, useState } from 'react';
import { AlertTriangle, CheckCircle2, Loader2, RefreshCw, Send } from 'lucide-react';
import { FulfillmentOrder, ShippingAccountConfig, getFulfillmentOrders, getShippingAccounts, saveShippingAccount, shippingPrecheck, submitPhysicalShipment, syncShippingAccountAddresses } from '../services/api';

const ShippingWorkbench: React.FC = () => {
  const [rows, setRows] = useState<FulfillmentOrder[]>([]);
  const [accounts, setAccounts] = useState<ShippingAccountConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [filter, setFilter] = useState('pending');
  const [message, setMessage] = useState('');
  const [shipping, setShipping] = useState('');

  const load = async () => {
    setLoading(true);
    try {
      const [orders, configs] = await Promise.all([getFulfillmentOrders(), getShippingAccounts()]);
      setRows(orders); setAccounts(configs);
    } finally { setLoading(false); }
  };
  useEffect(() => { void load(); }, []);

  const visible = useMemo(() => rows.filter(row => filter === 'all' ||
    (filter === 'pending' && row.pdd_shipped && !row.xianyu_shipped) ||
    (filter === 'sync' && !row.pdd_shipped && !!row.pdd_order_id) ||
    (filter === 'done' && row.xianyu_shipped) || (filter === 'problem' && !!row.last_error)), [rows, filter]);

  const ship = async (row: FulfillmentOrder) => {
    setMessage(''); setShipping(row.order_id);
    try {
      const check = await shippingPrecheck(row.order_id);
      if (!check.ready) { setMessage(check.problems.join('；')); return; }
		const result = await submitPhysicalShipment(row.order_id, `ship-v2-${row.order_id}-${row.tracking_number}`);
      setMessage(result.status === 'success' ? '闲鱼发货成功' : result.error || `发货状态：${result.status || '未知'}`);
      await load();
    } catch (error) { setMessage(error instanceof Error ? error.message : String(error)); }
    finally { setShipping(''); }
  };

  const saveAddress = async (account: ShippingAccountConfig, value: string) => {
    const id = Number(value);
    if (!Number.isSafeInteger(id) || id <= 0) { setMessage('发货地址 ID 无效'); return; }
    const selected = account.addresses?.find(item => item.contact_id === id);
    const summary = selected ? `${selected.contact_name}｜${selected.province_name}${selected.city_name}${selected.district_name} ${selected.detail_address}` : account.address_summary;
    await saveShippingAccount(account.cookie_id, id, summary); setMessage('卖家发货地址已保存'); await load();
  };
  const syncAddresses = async (account: ShippingAccountConfig) => {
    setMessage('');
    try { const result = await syncShippingAccountAddresses(account.cookie_id); setMessage(`已同步 ${result.count} 条卖家发货地址`); await load(); }
    catch (error) { setMessage(error instanceof Error ? error.message : String(error)); }
  };

  return <div className="space-y-6">
    <div className="flex flex-wrap items-center justify-between gap-4"><div><h1 className="text-3xl font-black text-gray-900">发货工作台</h1><p className="mt-2 text-sm text-gray-500">拼多多物流同步、核对与闲鱼实物发货</p></div><button onClick={load} className="px-4 py-2.5 rounded-xl bg-gray-900 text-white font-bold flex gap-2"><RefreshCw className="w-4 h-4"/>刷新</button></div>
    <div className="rounded-xl bg-amber-50 p-4 text-sm text-amber-800 flex gap-2"><AlertTriangle className="w-5 h-5 shrink-0"/><span>拼多多物流自动同步；闲鱼真实发货保持人工审核。预检全部通过后，仍需点击发货按钮才会提交。</span></div>
    {message && <div className={`rounded-xl p-4 text-sm ${message === '闲鱼发货成功' ? 'bg-green-50 text-green-700' : 'bg-red-50 text-red-700'}`}>{message}</div>}
    <div className="rounded-2xl bg-white border p-5"><h2 className="font-black mb-3">闲鱼账号卖家发货地址</h2><div className="grid md:grid-cols-2 gap-3">{accounts.map(account => <div key={account.cookie_id} className="rounded-xl bg-gray-50 p-4">
      <div className="flex items-center justify-between gap-2 mb-2"><div className="text-sm font-bold">{account.remark || account.cookie_id}</div><button onClick={() => syncAddresses(account)} className="px-3 py-1.5 rounded-lg bg-blue-50 text-blue-700 text-xs font-bold">同步地址</button></div>
		{account.addresses?.length ? <div className="mb-2 space-y-1 text-xs text-gray-500">{account.addresses.map(address => <div key={address.contact_id}>{address.contact_name}｜{address.province_name}{address.city_name}{address.district_name} {address.detail_address}{address.platform_default ? '（平台默认联系人）' : ''}</div>)}</div> : <div className="mb-2 text-xs text-gray-400">尚未同步联系人地址。</div>}
		<div className="text-xs text-amber-700 mb-2">发货 addressId 与上面的 contactId 不同，请填写闲鱼发货请求中实际使用的 addressId。</div><div className="flex gap-2"><input id={`address-${account.cookie_id}`} defaultValue={account.address_id || ''} placeholder="闲鱼发货 addressId" className="ios-input rounded-lg px-3 py-2 min-w-0 flex-1"/><button onClick={() => saveAddress(account, (document.getElementById(`address-${account.cookie_id}`) as HTMLInputElement)?.value || '')} className="px-3 rounded-lg bg-gray-900 text-white text-sm font-bold">保存</button></div>
    </div>)}</div></div>
    <div className="flex gap-2 flex-wrap">{Object.entries({ sync: '待同步物流', pending: '待审核发货', problem: '异常', done: '已发货', all: '全部' }).map(([key, label]) => <button key={key} onClick={() => setFilter(key)} className={`px-4 py-2 rounded-xl text-sm font-bold ${filter === key ? 'bg-blue-600 text-white' : 'bg-white border'}`}>{label}</button>)}</div>
    <div className="rounded-2xl bg-white border overflow-hidden">{loading ? <div className="p-12 flex justify-center"><Loader2 className="animate-spin"/></div> : visible.length === 0 ? <div className="p-12 text-center text-gray-400">暂无订单</div> : <div className="overflow-x-auto"><table className="w-full text-sm">
      <thead className="bg-gray-50 text-left"><tr><th className="p-4">闲鱼订单</th><th className="p-4">拼多多订单</th><th className="p-4">商品/SKU</th><th className="p-4">物流</th><th className="p-4">状态</th><th className="p-4"/></tr></thead>
      <tbody>{visible.map(row => <tr key={row.order_id} className="border-t"><td className="p-4 font-mono">{row.order_id}</td><td className="p-4 font-mono">{row.pdd_order_id || '-'}</td><td className="p-4"><div>{row.spec_name}：{row.spec_value}</div><div className="text-xs text-gray-400 font-mono">{row.source_goods_id} / {row.source_sku_id}</div></td><td className="p-4"><div>{row.logistics_company || '-'}</div><div className="font-mono text-xs">{row.tracking_number || '-'}</div></td><td className="p-4">{row.xianyu_shipped ? <span className="text-green-600 flex gap-1"><CheckCircle2 className="w-4"/>闲鱼已发货</span> : row.pdd_shipped ? <span className="font-bold text-amber-700">待人工审核发货</span> : '待同步物流'}</td><td className="p-4"><button disabled={!row.pdd_shipped || row.xianyu_shipped || shipping === row.order_id} onClick={() => ship(row)} className="px-3 py-2 rounded-lg bg-blue-600 text-white disabled:opacity-40 flex gap-1">{shipping === row.order_id ? <Loader2 className="w-4 animate-spin"/> : <Send className="w-4"/>}{shipping === row.order_id ? '处理中' : '审核并发货'}</button></td></tr>)}</tbody>
    </table></div>}</div>
  </div>;
};
export default ShippingWorkbench;
