import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  AlertTriangle, CheckCircle2, ChevronDown, ClipboardList, ExternalLink, Eye, Loader2,
  PackageCheck, RefreshCw, Search, ShieldCheck, Trash2, Truck, X,
} from 'lucide-react';
import {
  FulfillmentOrder, FulfillmentOrderFilters, FulfillmentOrderPatch,
  FulfillmentHistoryRepairPreview, getFulfillmentOrders, getOrderSyncStatus, OrderSyncStatus,
  PDDPurchaseTask, FulfillmentException, clearFulfillmentExceptions, confirmPDDPurchasePayment, confirmUnknownPurchaseCancelled, getFulfillmentExceptions, getPDDPurchaseTasks, readFulfillmentExceptions, requestFulfillmentPurchase, resolveFulfillmentException,
  previewFulfillmentHistoryRepair, repairFulfillmentHistory, updateFulfillmentOrder,
  getPDDAccount,
} from '../services/api';

type Preset = 'pending_order' | 'pending_pdd_ship' | 'pending_xianyu_ship' | 'pending_reminder' | 'all';

const presets: Array<{id: Preset; label: string; filters: FulfillmentOrderFilters}> = [
  { id: 'pending_order', label: '拼多多未下单', filters: { pdd_ordered: false } },
  { id: 'pending_pdd_ship', label: '拼多多未发货', filters: { pdd_paid: true, pdd_shipped: false } },
  { id: 'pending_xianyu_ship', label: '闲鱼未发货', filters: { pdd_shipped: true, xianyu_shipped: false } },
  { id: 'pending_reminder', label: '发货后未提醒', filters: { xianyu_shipped: true, reminded: false } },
  { id: 'all', label: '全部', filters: {} },
];

const mappingMeta = (status: string) => {
  switch (status) {
    case 'mapped': return { label: '映射正常', className: 'bg-green-50 text-green-700' };
    case 'ambiguous': return { label: '映射冲突', className: 'bg-amber-50 text-amber-700' };
    case 'unmapped': return { label: '未匹配 SKU', className: 'bg-red-50 text-red-700' };
    default: return { label: '等待映射', className: 'bg-gray-100 text-gray-600' };
  }
};

const formatTime = (timestamp: number) => timestamp
  ? new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(timestamp * 1000))
  : '-';

const shortID = (value: string) => value ? `${value.slice(0, 8)}${value.length > 12 ? '…' + value.slice(-4) : ''}` : '-';

const FulfillmentWorkbench: React.FC = () => {
  const [orders, setOrders] = useState<FulfillmentOrder[]>([]);
  const [preset, setPreset] = useState<Preset>('pending_order');
  const [mappingStatus, setMappingStatus] = useState('');
  const [search, setSearch] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState<FulfillmentOrder | null>(null);
  const [form, setForm] = useState<FulfillmentOrderPatch>({});
  const [saving, setSaving] = useState(false);
  const [syncStatus, setSyncStatus] = useState<OrderSyncStatus | null>(null);
  const [repairPreview, setRepairPreview] = useState<FulfillmentHistoryRepairPreview | null>(null);
  const [repairing, setRepairing] = useState(false);
  const [purchaseTasks, setPurchaseTasks] = useState<PDDPurchaseTask[]>([]);
  const [exceptions, setExceptions] = useState<FulfillmentException[]>([]);
  const [taskBusy, setTaskBusy] = useState('');
  const [tasksOpen, setTasksOpen] = useState(() => localStorage.getItem('fulfillment.tasks.open') === 'true');
  const [exceptionsOpen, setExceptionsOpen] = useState(() => localStorage.getItem('fulfillment.exceptions.open') === 'true');
  const [pddBaseURL, setPDDBaseURL] = useState('https://mobile.pinduoduo.com');
  const previousUnread = useRef(0);

  const loadOrders = async () => {
    setLoading(true);
    setError('');
    try {
      const selected = presets.find(item => item.id === preset)?.filters || {};
      const [nextOrders, nextStatus, nextTasks, nextExceptions, pddAccount] = await Promise.all([
        getFulfillmentOrders({ ...selected, mapping_status: mappingStatus || undefined }),
        getOrderSyncStatus(),
        getPDDPurchaseTasks(),
        getFulfillmentExceptions(),
        getPDDAccount(),
      ]);
      setOrders(nextOrders);
      setSyncStatus(nextStatus);
      setPurchaseTasks(nextTasks);
      setExceptions(nextExceptions);
      setPDDBaseURL(pddAccount.base_url || 'https://mobile.pinduoduo.com');
    } catch (err) {
      setError((err as Error).message || '读取履约订单失败');
    } finally {
      setLoading(false);
    }
  };

  const confirmPayment = async (task: PDDPurchaseTask) => {
    setTaskBusy(task.id); setError('');
    try { await confirmPDDPurchasePayment(task.id, task.pdd_order_id); await loadOrders(); }
    catch (err) { setError((err as Error).message || '确认拼多多付款失败'); }
    finally { setTaskBusy(''); }
  };
  const confirmCancelled = async (task:PDDPurchaseTask) => {
    if (!window.confirm('仅在确认旧拼多多订单已经取消时继续。确认后将重新进入采购队列，是否继续？')) return;
    setTaskBusy(task.id); setError('');
    try { await confirmUnknownPurchaseCancelled(task.id); await loadOrders(); }
    catch(err) { setError((err as Error).message || '确认旧订单取消失败'); }
    finally { setTaskBusy(''); }
  };

  const requestPurchase = async (order: FulfillmentOrder) => {
    setTaskBusy(order.order_id); setError('');
    try { await requestFulfillmentPurchase(order.order_id); await loadOrders(); }
    catch (err) { setError((err as Error).message || '加入采购队列失败'); }
    finally { setTaskBusy(''); }
  };

  const clearExceptionLogs = async () => {
    if (!window.confirm('清空全部履约异常日志？真实发货、地址修改和 SKU 映射审计不会删除。')) return;
    setTaskBusy('clear-logs'); setError('');
    try { await clearFulfillmentExceptions('all'); await loadOrders(); }
    catch (err) { setError((err as Error).message || '清空异常日志失败'); }
    finally { setTaskBusy(''); }
  };
  const resolveException = async (id:string) => { setTaskBusy(id); try { await resolveFulfillmentException(id); await loadOrders(); } catch(err) { setError((err as Error).message || '标记异常失败'); } finally { setTaskBusy(''); } };
  const markExceptionsRead = async () => {
    setTaskBusy('read-logs'); setError('');
    try { await readFulfillmentExceptions(); await loadOrders(); }
    catch (err) { setError((err as Error).message || '标记异常已读失败'); }
    finally { setTaskBusy(''); }
  };

  useEffect(() => { void loadOrders(); }, [preset, mappingStatus]);
  useEffect(() => { localStorage.setItem('fulfillment.tasks.open', String(tasksOpen)); }, [tasksOpen]);
  useEffect(() => { localStorage.setItem('fulfillment.exceptions.open', String(exceptionsOpen)); }, [exceptionsOpen]);
  const unreadExceptions = useMemo(() => exceptions.filter(item => !item.read_at).length, [exceptions]);
  useEffect(() => {
    if (unreadExceptions > previousUnread.current) setExceptionsOpen(true);
    previousUnread.current = unreadExceptions;
  }, [unreadExceptions]);

  const visibleOrders = useMemo(() => {
    const needle = search.trim().toLowerCase();
    if (!needle) return orders;
    return orders.filter(order => [
      order.order_id, order.pdd_order_id, order.item_id, order.source_goods_id,
      order.source_sku_id, order.xianyu_sku_id, order.spec_name, order.spec_value,
      order.receiver_name, order.tracking_number,
    ].some(value => String(value || '').toLowerCase().includes(needle)));
  }, [orders, search]);

  const openEditor = (order: FulfillmentOrder) => {
    setEditing(order);
    setForm({
      pdd_ordered: order.pdd_ordered,
      pdd_paid: order.pdd_paid,
      pdd_order_id: order.pdd_order_id,
      pdd_shipped: order.pdd_shipped,
      logistics_company: order.logistics_company,
      tracking_number: order.tracking_number,
      xianyu_shipped: order.xianyu_shipped,
      reminded: order.reminded,
    });
  };

  const save = async () => {
    if (!editing) return;
    if (form.pdd_ordered && !form.pdd_order_id?.trim()) {
      setError('标记拼多多已下单时必须填写拼多多订单号');
      return;
    }
    if (form.pdd_shipped && (!form.logistics_company?.trim() || !form.tracking_number?.trim())) {
      setError('标记拼多多已发货时必须填写物流公司和快递单号');
      return;
    }
    setSaving(true);
    setError('');
    try {
      await updateFulfillmentOrder(editing.order_id, form);
      setEditing(null);
      await loadOrders();
    } catch (err) {
      setError((err as Error).message || '更新履约状态失败');
    } finally {
      setSaving(false);
    }
  };

  const openRepairPreview = async () => {
    setRepairing(true);
    setError('');
    try { setRepairPreview(await previewFulfillmentHistoryRepair()); }
    catch (err) { setError((err as Error).message || '生成历史订单修复预览失败'); }
    finally { setRepairing(false); }
  };

  const repairHistory = async () => {
    setRepairing(true);
    setError('');
    try {
      const result = await repairFulfillmentHistory();
      setRepairPreview(null);
      await loadOrders();
      setError(result.updated > 0 ? '' : '没有符合安全条件的历史订单需要修复');
    } catch (err) {
      setError((err as Error).message || '修复历史订单失败');
    } finally {
      setRepairing(false);
    }
  };

  const counts = useMemo(() => ({
    total: orders.length,
    unmapped: orders.filter(order => order.mapping_status !== 'mapped').length,
    shipped: orders.filter(order => order.pdd_shipped).length,
  }), [orders]);

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-4">
          <div className="w-12 h-12 bg-blue-600 rounded-2xl flex items-center justify-center text-white"><ClipboardList className="w-6 h-6" /></div>
          <div>
            <h2 className="text-3xl font-extrabold text-gray-900">订单履约工作台</h2>
            <p className="text-sm font-medium text-gray-500 mt-1">闲鱼订单、拼多多下单、物流和提醒状态统一管理</p>
            <p className="text-xs text-gray-400 mt-1">自动同步：{syncStatus?.enabled ? `每 ${syncStatus.interval_minutes} 分钟` : '未启用'} · 上次完成 {formatTime(syncStatus?.last_run?.finished_at || 0)}</p>
          </div>
        </div>
        <div className="flex flex-wrap gap-2"><button type="button" onClick={openRepairPreview} disabled={repairing} className="px-4 py-2.5 rounded-xl bg-amber-50 hover:bg-amber-100 text-amber-800 font-bold flex items-center justify-center gap-2 disabled:opacity-50"><ShieldCheck className="w-4 h-4" />修复历史订单</button><button type="button" onClick={loadOrders} disabled={loading} className="px-4 py-2.5 rounded-xl bg-gray-100 hover:bg-gray-200 text-gray-700 font-bold flex items-center justify-center gap-2 disabled:opacity-50"><RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />同步并刷新</button></div>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <div className="ios-card rounded-xl bg-white p-5"><div className="text-xs font-bold text-gray-400">当前结果</div><div className="mt-2 text-3xl font-black text-gray-900">{counts.total}</div></div>
        <div className="ios-card rounded-xl bg-white p-5"><div className="text-xs font-bold text-gray-400">SKU 映射待处理</div><div className="mt-2 text-3xl font-black text-amber-600">{counts.unmapped}</div></div>
        <div className="ios-card rounded-xl bg-white p-5"><div className="text-xs font-bold text-gray-400">拼多多已发货</div><div className="mt-2 text-3xl font-black text-green-600">{counts.shipped}</div></div>
      </div>

      <div className="ios-card rounded-xl bg-white p-4 space-y-4">
        <div className="flex flex-wrap gap-2">
          {presets.map(item => (
            <button key={item.id} type="button" onClick={() => setPreset(item.id)} className={`px-3.5 py-2 rounded-xl text-sm font-bold transition-colors ${preset === item.id ? 'bg-blue-600 text-white' : 'bg-gray-100 text-gray-600 hover:bg-gray-200'}`}>
              {item.label}
            </button>
          ))}
        </div>
        <div className="grid grid-cols-1 md:grid-cols-[1fr_220px] gap-3">
          <div className="relative">
            <Search className="absolute left-4 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
            <input value={search} onChange={event => setSearch(event.target.value)} className="w-full ios-input rounded-xl py-3 pl-11 pr-4 text-sm" placeholder="搜索闲鱼订单、拼多多订单、goods_id、SKU、快递单号" />
          </div>
          <select value={mappingStatus} onChange={event => setMappingStatus(event.target.value)} className="ios-input rounded-xl px-4 py-3 text-sm">
            <option value="">全部映射状态</option>
            <option value="mapped">映射正常</option>
            <option value="pending">等待映射</option>
            <option value="unmapped">未匹配 SKU</option>
            <option value="ambiguous">映射冲突</option>
          </select>
        </div>
      </div>

      {error && <div className="rounded-xl bg-red-50 px-4 py-3 text-sm font-medium text-red-700 flex items-start gap-2"><AlertTriangle className="w-4 h-4 mt-0.5" />{error}</div>}

      <div className="ios-card rounded-xl bg-white overflow-hidden">
        {loading ? (
          <div className="py-20 flex items-center justify-center text-gray-400"><Loader2 className="w-6 h-6 animate-spin mr-2" />正在同步履约订单</div>
        ) : visibleOrders.length === 0 ? (
          <div className="py-20 text-center text-gray-400">当前筛选条件下没有订单</div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[1100px] text-left">
              <thead className="bg-gray-50 text-xs text-gray-500"><tr>
                <th className="px-5 py-4">闲鱼订单 / 商品</th><th className="px-4 py-4">规格与映射</th><th className="px-4 py-4">拼多多订单</th><th className="px-4 py-4">物流</th><th className="px-4 py-4">流程状态</th><th className="px-5 py-4 text-right">操作</th>
              </tr></thead>
              <tbody className="divide-y divide-gray-100">
                {visibleOrders.map(order => {
                  const mapping = mappingMeta(order.mapping_status);
                  return <tr key={order.order_id} className="hover:bg-blue-50/30 align-top">
                    <td className="px-5 py-4"><div className="font-mono text-sm font-bold text-gray-900" title={order.order_id}>{shortID(order.order_id)}</div><div className="mt-1 text-xs text-gray-500">商品 {shortID(order.item_id)}</div><div className="mt-1 text-xs text-gray-400">账号 {shortID(order.cookie_id)}</div></td>
                    <td className="px-4 py-4"><span className={`inline-flex px-2.5 py-1 rounded-lg text-xs font-bold ${mapping.className}`}>{mapping.label}</span><div className="mt-2 text-sm text-gray-700">{order.spec_name && `${order.spec_name}：`}{order.spec_value || '未记录规格'}</div><div className="mt-1 font-mono text-xs text-gray-400">PDD SKU {shortID(order.source_sku_id)}</div></td>
                    <td className="px-4 py-4"><div className={`text-sm font-bold ${order.fulfillment_exempt ? 'text-blue-600' : order.pdd_paid ? 'text-green-700' : order.pdd_ordered ? 'text-amber-600' : 'text-gray-400'}`}>{order.fulfillment_exempt ? '无需履约' : order.pdd_paid ? '已付款 · 待发货' : order.pdd_ordered ? '已下单 · 待付款' : '未下单'}</div><div className="mt-1 font-mono text-xs text-gray-500">{order.pdd_order_id || '-'}</div>{order.pdd_order?.amount_cent != null && <div className="mt-2 text-xs text-gray-600">采购 ¥{(order.pdd_order.amount_cent / 100).toFixed(2)} · 数量 {order.pdd_order.quantity || 1}</div>}{order.pdd_order?.sku_id && <div className="mt-1 font-mono text-[11px] text-gray-400">SKU {order.pdd_order.sku_id}</div>}{order.pdd_order?.payment_deadline && !order.pdd_paid ? <div className="mt-1 text-[11px] text-amber-600">付款截止 {formatTime(order.pdd_order.payment_deadline)}</div> : null}{order.pdd_paid && <div className="mt-1 text-[11px] text-green-600">{order.pdd_paid_source === 'manual' ? '人工确认付款' : order.pdd_paid_source === 'auto_pdd_pending_ship' ? '待发货列表自动确认' : '已确认付款'} · {formatTime(order.pdd_paid_at)}</div>}{order.pdd_order?.receiver_name && <div className="mt-1 text-[11px] text-gray-400">{order.pdd_order.receiver_name} · {[order.pdd_order.province, order.pdd_order.city, order.pdd_order.district].filter(Boolean).join('')}</div>}{order.source_goods_id && <a href={`${pddBaseURL}/goods.html?goods_id=${encodeURIComponent(order.source_goods_id)}`} target="_blank" rel="noreferrer" className="mt-2 inline-flex items-center gap-1 text-xs font-bold text-blue-600">打开拼多多商品 <ExternalLink className="w-3 h-3" /></a>}</td>
                    <td className="px-4 py-4"><div className={`text-sm font-bold ${order.pdd_shipped ? 'text-green-700' : 'text-gray-400'}`}>{order.pdd_shipped ? '拼多多已发货' : '等待拼多多发货'}</div><div className="mt-1 text-xs text-gray-500">{order.logistics_company || '-'} · {order.tracking_number || '-'}</div></td>
                    <td className="px-4 py-4 space-y-1.5"><div className="flex items-center gap-1.5 text-xs"><PackageCheck className={`w-3.5 h-3.5 ${order.fulfillment_exempt || order.xianyu_shipped ? 'text-green-600' : 'text-gray-300'}`} />{order.fulfillment_exempt ? '无需继续履约' : `闲鱼${order.xianyu_shipped ? '已发货' : '未发货'}`}</div><div className="flex items-center gap-1.5 text-xs"><CheckCircle2 className={`w-3.5 h-3.5 ${order.reminder_exempt || order.reminded ? 'text-green-600' : 'text-gray-300'}`} />{order.reminder_exempt ? '无需提醒' : order.reminded ? '已提醒' : '未提醒'}</div><div className="text-[11px] text-gray-400">更新 {formatTime(order.updated_at)}</div></td>
                    <td className="px-5 py-4 text-right"><div className="flex flex-col items-end gap-2">{!order.fulfillment_exempt && !order.pdd_ordered && !order.pdd_order_id && <button type="button" onClick={() => requestPurchase(order)} disabled={taskBusy === order.order_id || order.mapping_status !== 'mapped' || order.purchase_requested_at > 0} className="px-3.5 py-2 rounded-lg bg-blue-600 hover:bg-blue-700 text-white text-xs font-bold disabled:opacity-45">{taskBusy === order.order_id ? '处理中…' : order.purchase_requested_at > 0 ? '已优先排队' : '立即下单'}</button>}<button type="button" onClick={() => openEditor(order)} className="px-3.5 py-2 rounded-lg bg-gray-900 hover:bg-black text-white text-xs font-bold">更新履约</button></div></td>
                  </tr>;
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <div className="ios-card rounded-xl bg-white p-4">
        <button type="button" onClick={() => setTasksOpen(value => !value)} className="flex w-full items-center justify-between gap-3 text-left"><div><div className="font-black text-gray-900">采购任务 <span className="ml-2 text-xs font-bold text-gray-400">{purchaseTasks.length} 条 · 异常 {purchaseTasks.filter(item => item.last_error).length}</span></div><div className="mt-1 text-xs text-gray-400">只负责拼多多下单与订单号回填；物流、闲鱼发货和提醒由履约订单继续跟踪。</div></div><ChevronDown className={`h-5 w-5 shrink-0 text-gray-400 transition-transform ${tasksOpen ? 'rotate-180' : ''}`} /></button>
        {tasksOpen && <div className="mt-3">{purchaseTasks.length === 0 ? <div className="rounded-xl bg-gray-50 px-4 py-8 text-center text-sm text-gray-400">暂无采购任务</div> : <div className="space-y-2">{purchaseTasks.slice(0, 20).map(task => <div key={task.id} className={`flex flex-wrap items-center justify-between gap-3 rounded-xl px-4 py-3 text-sm ${task.last_error ? 'bg-red-50' : 'bg-gray-50'}`}><div><span className="font-mono font-bold">{shortID(task.order_id)}</span><span className="ml-3 text-gray-500">第 {task.attempt} 次 · {task.status}</span>{task.pdd_order_id && <span className="ml-3 font-mono text-gray-500">{task.pdd_order_id}</span>}{task.last_error && <div className="mt-1 text-xs text-red-600">{task.last_error}</div>}</div><div className="flex gap-2">{task.status === 'result_unknown' && !task.pdd_order_id && <button type="button" onClick={()=>confirmCancelled(task)} disabled={taskBusy===task.id} className="rounded-lg bg-amber-600 px-3 py-2 text-xs font-bold text-white disabled:opacity-50">旧订单已取消，重新下单</button>}{task.status === 'completed' && task.pdd_order_id && <button type="button" onClick={() => confirmPayment(task)} disabled={taskBusy === task.id} className="rounded-lg bg-green-600 px-3 py-2 text-xs font-bold text-white disabled:opacity-50">{taskBusy === task.id ? '确认中…' : '确认已付款'}</button>}</div></div>)}</div>}</div>}
      </div>

      <div className="ios-card rounded-xl bg-white p-4">
        <div className="flex flex-wrap items-center justify-between gap-3"><button type="button" onClick={() => setExceptionsOpen(value => !value)} className="flex min-w-0 flex-1 items-center justify-between gap-3 text-left"><div><div className="font-black text-gray-900">异常日志 {unreadExceptions > 0 && <span className="ml-2 rounded-full bg-red-600 px-2 py-0.5 text-[11px] text-white">{unreadExceptions} 未读</span>}</div><div className="mt-1 text-xs text-gray-400">已读只消除提醒；已解决才表示异常处理完成。</div></div><ChevronDown className={`h-5 w-5 shrink-0 text-gray-400 transition-transform ${exceptionsOpen ? 'rotate-180' : ''}`} /></button><div className="flex gap-2"><button type="button" onClick={markExceptionsRead} disabled={unreadExceptions === 0 || taskBusy === 'read-logs'} className="inline-flex items-center gap-2 rounded-lg bg-blue-50 px-3 py-2 text-xs font-bold text-blue-700 disabled:opacity-40"><Eye className="h-3.5 w-3.5" />一键已读</button><button type="button" onClick={clearExceptionLogs} disabled={exceptions.length === 0 || taskBusy === 'clear-logs'} className="inline-flex items-center gap-2 rounded-lg bg-red-50 px-3 py-2 text-xs font-bold text-red-700 disabled:opacity-40"><Trash2 className="h-3.5 w-3.5" />清空</button></div></div>
        {exceptionsOpen && <div className="mt-3">{exceptions.length === 0 ? <div className="rounded-xl bg-green-50 px-4 py-8 text-center text-sm text-green-700">当前没有异常</div> : <div className="space-y-2">{exceptions.slice(0, 50).map(item => <div key={item.id} className={`rounded-xl px-4 py-3 ${item.status==='resolved'?'bg-gray-50':item.read_at?'bg-amber-50':'bg-red-50'}`}><div className="flex flex-wrap items-center justify-between gap-2"><span className={`text-xs font-bold ${item.status==='resolved'?'text-gray-500':item.read_at?'text-amber-700':'text-red-700'}`}>{item.event_type}{!item.read_at && <span className="ml-2">· 未读</span>}</span><div className="flex items-center gap-2"><span className="text-[11px] text-gray-400">{formatTime(item.created_at)}</span>{item.status==='open'&&<button onClick={()=>resolveException(item.id)} disabled={taskBusy===item.id} className="rounded bg-white px-2 py-1 text-[11px] font-bold text-gray-700">标记已解决</button>}</div></div><div className={`mt-1 text-sm ${item.status==='resolved'?'text-gray-500':item.read_at?'text-amber-900':'text-red-800'}`}>{item.summary}</div><div className="mt-1 font-mono text-[11px] text-gray-500">订单 {item.order_id || '-'}{item.task_id ? ` · 任务 ${shortID(item.task_id)}` : ''}</div></div>)}</div>}</div>}
      </div>

      {editing && (
        <div className="fixed inset-0 z-50 bg-black/35 backdrop-blur-sm flex items-center justify-center p-4" onMouseDown={event => { if (event.target === event.currentTarget) setEditing(null); }}>
          <div className="w-full max-w-2xl max-h-[90vh] overflow-y-auto rounded-2xl bg-white shadow-2xl">
            <div className="sticky top-0 bg-white border-b border-gray-100 px-6 py-5 flex items-center justify-between"><div><h3 className="text-xl font-black text-gray-900">更新订单履约</h3><p className="mt-1 font-mono text-xs text-gray-400">{editing.order_id}</p></div><button type="button" onClick={() => setEditing(null)} className="p-2 rounded-lg hover:bg-gray-100"><X className="w-5 h-5" /></button></div>
            <div className="p-6 space-y-6">
              {editing.mapping_status !== 'mapped' && <div className="rounded-xl bg-amber-50 p-4 text-sm text-amber-800 flex gap-2"><AlertTriangle className="w-4 h-4 mt-0.5 shrink-0" /><span>SKU 映射状态为“{mappingMeta(editing.mapping_status).label}”。自动下单前必须先处理映射；这里仅允许人工记录履约结果。</span></div>}
              <div className="space-y-3"><label className="flex items-center gap-3 font-bold text-sm"><input type="checkbox" checked={form.pdd_ordered === true} onChange={event => setForm({...form, pdd_ordered: event.target.checked})} className="w-4 h-4" />拼多多已经下单</label><label className="flex items-center gap-3 font-bold text-sm"><input type="checkbox" checked={form.pdd_paid === true} onChange={event => setForm({...form, pdd_paid: event.target.checked, pdd_ordered: event.target.checked ? true : form.pdd_ordered})} className="w-4 h-4" />拼多多已经付款</label><input value={form.pdd_order_id || ''} onChange={event => setForm({...form, pdd_order_id: event.target.value})} className="w-full ios-input rounded-xl px-4 py-3 text-sm font-mono" placeholder="拼多多订单号" /></div>
              <div className="space-y-3"><label className="flex items-center gap-3 font-bold text-sm"><input type="checkbox" checked={form.pdd_shipped === true} onChange={event => setForm({...form, pdd_shipped: event.target.checked})} className="w-4 h-4" />拼多多已经发货</label><div className="grid grid-cols-1 sm:grid-cols-2 gap-3"><input value={form.logistics_company || ''} onChange={event => setForm({...form, logistics_company: event.target.value})} className="ios-input rounded-xl px-4 py-3 text-sm" placeholder="物流公司" /><input value={form.tracking_number || ''} onChange={event => setForm({...form, tracking_number: event.target.value})} className="ios-input rounded-xl px-4 py-3 text-sm font-mono" placeholder="快递单号" /></div></div>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3"><label className="rounded-xl bg-gray-50 p-4 flex items-center gap-3 font-bold text-sm"><input type="checkbox" checked={form.xianyu_shipped === true} onChange={event => setForm({...form, xianyu_shipped: event.target.checked})} className="w-4 h-4" />闲鱼已经发货</label><label className="rounded-xl bg-gray-50 p-4 flex items-center gap-3 font-bold text-sm"><input type="checkbox" checked={form.reminded === true} onChange={event => setForm({...form, reminded: event.target.checked})} className="w-4 h-4" />客户已经提醒</label></div>
            </div>
            <div className="sticky bottom-0 bg-white border-t border-gray-100 px-6 py-4 flex justify-end gap-3"><button type="button" onClick={() => setEditing(null)} className="px-5 py-2.5 rounded-xl bg-gray-100 font-bold text-gray-700">取消</button><button type="button" onClick={save} disabled={saving} className="px-5 py-2.5 rounded-xl bg-blue-600 hover:bg-blue-700 text-white font-bold flex items-center gap-2 disabled:opacity-50">{saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Truck className="w-4 h-4" />}{saving ? '保存中...' : '保存履约状态'}</button></div>
          </div>
        </div>
      )}

      {repairPreview && (
        <div className="fixed inset-0 z-50 bg-black/35 backdrop-blur-sm flex items-center justify-center p-4" onMouseDown={event => { if (event.target === event.currentTarget) setRepairPreview(null); }}>
          <div className="w-full max-w-lg rounded-2xl bg-white shadow-2xl p-6 space-y-5">
            <div><h3 className="text-xl font-black text-gray-900">修复历史订单预览</h3><p className="mt-2 text-sm text-gray-500">只将安全的交易完成/已取消旧订单标记为“无需履约、无需提醒”。该操作不会修改平台订单数据。</p></div>
            <div className="grid grid-cols-2 gap-3 text-sm"><div className="rounded-xl bg-green-50 p-4"><div className="text-green-700">可安全修复</div><div className="mt-1 text-2xl font-black text-green-800">{repairPreview.eligible}</div></div><div className="rounded-xl bg-gray-50 p-4"><div className="text-gray-500">活跃订单排除</div><div className="mt-1 text-2xl font-black text-gray-800">{repairPreview.active_excluded}</div></div><div className="rounded-xl bg-gray-50 p-4"><div className="text-gray-500">人工修改排除</div><div className="mt-1 text-2xl font-black text-gray-800">{repairPreview.manual_excluded}</div></div><div className="rounded-xl bg-gray-50 p-4"><div className="text-gray-500">已有拼多多单号排除</div><div className="mt-1 text-2xl font-black text-gray-800">{repairPreview.pdd_excluded}</div></div></div>
            <div className="rounded-xl bg-amber-50 p-4 text-xs leading-6 text-amber-800">待处理、待发货、退款中、已发货/待收货订单全部排除；已有拼多多订单号或人工履约修改的订单也不会处理。</div>
            <div className="flex justify-end gap-3"><button type="button" onClick={() => setRepairPreview(null)} className="px-5 py-2.5 rounded-xl bg-gray-100 font-bold text-gray-700">取消</button><button type="button" onClick={repairHistory} disabled={repairing || repairPreview.eligible === 0} className="px-5 py-2.5 rounded-xl bg-amber-600 text-white font-bold disabled:opacity-50 flex items-center gap-2">{repairing && <Loader2 className="w-4 h-4 animate-spin" />}确认修复 {repairPreview.eligible} 笔</button></div>
          </div>
        </div>
      )}
    </div>
  );
};

export default FulfillmentWorkbench;
