import React, { useState } from 'react';
import { Laptop, PackageSearch } from 'lucide-react';
import PDDCollectedProducts from './PDDCollectedProducts';
import PDDCollectorDevices from './PDDCollectorDevices';

const PDDCollector: React.FC = () => {
  const [tab, setTab] = useState<'products' | 'devices'>('products');
  return <div className="space-y-6">
    <div><h1 className="text-3xl font-black text-slate-950">拼多多采集</h1><p className="mt-2 text-slate-500">管理采集商品、SKU 与浏览器设备。</p></div>
    <div className="flex w-fit gap-1 rounded-xl bg-slate-100 p-1">
      <button onClick={() => setTab('products')} className={`flex items-center gap-2 rounded-lg px-4 py-2 font-bold ${tab === 'products' ? 'bg-white text-brand shadow-sm' : 'text-slate-500'}`}><PackageSearch className="h-4 w-4" />商品与 SKU</button>
      <button onClick={() => setTab('devices')} className={`flex items-center gap-2 rounded-lg px-4 py-2 font-bold ${tab === 'devices' ? 'bg-white text-brand shadow-sm' : 'text-slate-500'}`}><Laptop className="h-4 w-4" />采集设备</button>
    </div>
    {tab === 'products' ? <PDDCollectedProducts /> : <PDDCollectorDevices compact />}
  </div>;
};
export default PDDCollector;
