import React, { useEffect, useState } from 'react';
import { Check, Clipboard, KeyRound, Laptop, Plus, RefreshCw } from 'lucide-react';
import { createPDDCollectorDevice, getPDDCollectorDevices, PDDCollectorDevice } from '../services/api';

const formatTime = (value: number) => value ? new Date(value * 1000).toLocaleString() : '从未';

const PDDCollectorDevices: React.FC<{ compact?: boolean }> = ({ compact = false }) => {
  const [devices, setDevices] = useState<PDDCollectorDevice[]>([]);
  const [name, setName] = useState('');
  const [token, setToken] = useState('');
  const [copied, setCopied] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const load = async () => {
    setLoading(true); setError('');
    try { setDevices(await getPDDCollectorDevices()); }
    catch (err) { setError(err instanceof Error ? err.message : '加载设备失败'); }
    finally { setLoading(false); }
  };
  useEffect(() => { void load(); }, []);

  const create = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!name.trim()) return;
    setSaving(true); setError(''); setToken('');
    try {
      const result = await createPDDCollectorDevice(name.trim());
      setToken(result.device_token); setName(''); await load();
    } catch (err) { setError(err instanceof Error ? err.message : '创建设备失败'); }
    finally { setSaving(false); }
  };

  const copy = async () => {
    await navigator.clipboard.writeText(token); setCopied(true);
    window.setTimeout(() => setCopied(false), 1500);
  };

  return <div className="space-y-6">
    {!compact && <div className="flex items-center justify-between">
      <div><h1 className="text-3xl font-black text-slate-950">拼多多采集设备</h1><p className="mt-2 text-slate-500">绑定浏览器扩展，采集商品后直接上传数据库。</p></div>
      <button onClick={() => void load()} className="flex items-center gap-2 rounded-xl border border-slate-200 bg-white px-4 py-2 font-bold text-slate-600"><RefreshCw className="h-4 w-4" />刷新</button>
    </div>}

    <form onSubmit={create} className="rounded-2xl border border-slate-200 bg-white p-6 shadow-sm">
      <h2 className="mb-4 flex items-center gap-2 text-lg font-black"><Plus className="h-5 w-5 text-brand" />创建采集设备</h2>
      <div className="flex gap-3"><input value={name} onChange={e => setName(e.target.value)} maxLength={100} placeholder="例如：运营电脑1" className="min-w-0 flex-1 rounded-xl border border-slate-200 px-4 py-3 outline-none focus:border-brand" /><button disabled={saving || !name.trim()} className="rounded-xl bg-brand px-6 py-3 font-bold text-white disabled:opacity-50">{saving ? '创建中…' : '创建并生成 Token'}</button></div>
    </form>

    {token && <section className="rounded-2xl border border-amber-200 bg-amber-50 p-6">
      <div className="flex items-center gap-2 font-black text-amber-900"><KeyRound className="h-5 w-5" />设备 Token 仅显示一次</div>
      <p className="my-3 text-sm text-amber-800">复制到扩展设置页。服务端只保存哈希，关闭后无法再次查看。</p>
      <div className="flex gap-2"><code className="min-w-0 flex-1 overflow-x-auto rounded-xl bg-white px-4 py-3 text-sm">{token}</code><button onClick={() => void copy()} className="flex items-center gap-2 rounded-xl bg-slate-900 px-4 text-white">{copied ? <Check className="h-4 w-4" /> : <Clipboard className="h-4 w-4" />}{copied ? '已复制' : '复制'}</button></div>
    </section>}

    {error && <div className="rounded-xl bg-red-50 p-4 font-bold text-red-600">{error}</div>}
    <section className="overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm">
      <div className="border-b border-slate-100 px-6 py-4 font-black">已绑定设备</div>
      {loading ? <div className="p-8 text-center text-slate-500">加载中…</div> : devices.length === 0 ? <div className="p-10 text-center text-slate-500">尚未创建设备</div> : <div className="divide-y divide-slate-100">{devices.map(device => <div key={device.id} className="grid gap-4 px-6 py-5 md:grid-cols-[1fr_120px_200px_200px] md:items-center">
        <div className="flex items-center gap-3"><span className="rounded-xl bg-blue-50 p-3 text-brand"><Laptop className="h-5 w-5" /></span><div><div className="font-black">{device.name}</div><div className="text-xs text-slate-400">{device.id}</div></div></div>
        <span className={`w-fit rounded-full px-3 py-1 text-xs font-black ${device.enabled ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-500'}`}>{device.enabled ? '已启用' : '已停用'}</span>
        <div><div className="text-xs text-slate-400">最后在线</div><div className="text-sm font-bold">{formatTime(device.last_seen_at)}</div></div>
        <div><div className="text-xs text-slate-400">最后采集</div><div className="text-sm font-bold">{formatTime(device.last_collected_at)}</div></div>
      </div>)}</div>}
    </section>
  </div>;
};
export default PDDCollectorDevices;
