import React, { useEffect, useState } from 'react';
import { Clock3, Loader2, Play, RefreshCw } from 'lucide-react';
import { getOrderSyncStatus, OrderSyncStatus, syncOrders } from '../services/api';
import { SystemSettings } from '../types';

interface Props {
  settings: SystemSettings;
  onChange: (next: SystemSettings) => void;
}

const formatTime = (timestamp?: number) => timestamp
  ? new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }).format(new Date(timestamp * 1000))
  : '-';

const statusLabel = (status?: string) => ({ success: '成功', partial: '部分失败', failed: '失败' }[status || ''] || '暂无记录');

const OrderSyncSettings: React.FC<Props> = ({ settings, onChange }) => {
  const [status, setStatus] = useState<OrderSyncStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [running, setRunning] = useState(false);
  const [message, setMessage] = useState('');

  const loadStatus = async () => {
    setLoading(true);
    try { setStatus(await getOrderSyncStatus()); }
    catch (error) { setMessage((error as Error).message || '读取同步状态失败'); }
    finally { setLoading(false); }
  };

  useEffect(() => { void loadStatus(); }, []);

  const runNow = async () => {
    setRunning(true);
    setMessage('');
    try {
      const result = await syncOrders();
      setMessage(result.message || '订单同步完成');
      await loadStatus();
    } catch (error) {
      setMessage((error as Error).message || '订单同步失败');
    } finally {
      setRunning(false);
    }
  };

  const enabled = settings.order_sync_enabled === true || String(settings.order_sync_enabled).toLowerCase() === 'true';
  return (
    <section className="space-y-4">
      <h3 className="text-lg font-extrabold text-gray-800 flex items-center gap-2"><div className="p-1.5 rounded-lg bg-cyan-600 text-white"><Clock3 className="w-4 h-4" /></div>订单自动同步</h3>
      <div className="ios-card rounded-xl p-6 bg-white space-y-5">
        <label className="flex items-start justify-between gap-5 rounded-xl bg-cyan-50 p-4 cursor-pointer">
          <span><span className="block text-sm font-bold text-cyan-950">启用自动订单同步</span><span className="block mt-1 text-xs text-cyan-700">按间隔同步全部启用的闲鱼账号，并自动更新履约记录。</span></span>
          <input type="checkbox" checked={enabled} onChange={event => onChange({...settings, order_sync_enabled: event.target.checked})} className="mt-1 w-5 h-5 rounded" />
        </label>
        <div className="space-y-2"><label className="block text-sm font-bold text-gray-800">同步间隔（分钟）</label><input type="number" min={5} max={1440} value={settings.order_sync_interval_minutes ?? 10} onChange={event => onChange({...settings, order_sync_interval_minutes: Math.max(5, Math.min(1440, Number(event.target.value) || 10))})} className="w-full ios-input px-4 py-3 rounded-xl" /><p className="text-xs text-gray-500">允许 5～1440 分钟，推荐 10 分钟。保存设置后自动生效，无需重启。</p></div>
        <div className="rounded-xl bg-gray-50 p-4 grid grid-cols-1 sm:grid-cols-2 gap-3 text-xs">
          <div><span className="text-gray-400">运行状态</span><div className="mt-1 font-bold text-gray-800">{status?.running ? '正在同步' : statusLabel(status?.last_run?.status)}</div></div>
          <div><span className="text-gray-400">上次完成</span><div className="mt-1 font-bold text-gray-800">{formatTime(status?.last_run?.finished_at)}</div></div>
          <div><span className="text-gray-400">下次计划</span><div className="mt-1 font-bold text-gray-800">{enabled ? formatTime(status?.next_run_at) : '自动同步未启用'}</div></div>
          <div><span className="text-gray-400">上次结果</span><div className="mt-1 font-bold text-gray-800">新增 {status?.last_run?.discovered || 0} · 更新 {status?.last_run?.updated || 0} · 履约 {status?.last_run?.fulfillment_updated || 0} · 失败 {status?.last_run?.failed || 0}</div></div>
        </div>
        {message && <div className="rounded-xl bg-blue-50 px-4 py-3 text-sm text-blue-700">{message}</div>}
        <div className="flex gap-2"><button type="button" onClick={runNow} disabled={running || status?.running} className="flex-1 rounded-xl bg-cyan-600 hover:bg-cyan-700 px-4 py-3 text-white font-bold text-sm flex items-center justify-center gap-2 disabled:opacity-50">{running ? <Loader2 className="w-4 h-4 animate-spin" /> : <Play className="w-4 h-4" />}{running ? '同步中...' : '立即同步全部账号'}</button><button type="button" onClick={loadStatus} disabled={loading} className="rounded-xl bg-gray-100 px-4 text-gray-600"><RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} /></button></div>
      </div>
    </section>
  );
};

export default OrderSyncSettings;
