import React, {useEffect, useState} from 'react';
import {Activity, CheckCircle2, Eye, EyeOff, RefreshCw, Save, Trash2} from 'lucide-react';
import {deletePDDAccount, getPDDAccount, getPDDAccountEvents, getPDDAccountRuntime, pausePDDAccount, resumePDDAccount, savePDDAccount, verifyPDDAccount} from '../services/api';
import type {PDDAccountConfig, PDDAccountEvent, PDDAccountRuntime} from '../types';

const timeText=(value:number)=>value?new Date(value*1000).toLocaleString():'-';
const operationText:Record<string,string>={purchase:'采购下单',logistics_sync:'物流同步',merchant_message:'客服操作'};
const statusText:Record<string,string>={valid:'正常',unchecked:'待验证',unconfigured:'未配置',paused:'已暂停',risk_blocked:'需要验证',invalid:'无效',expired:'已失效',unknown:'未知'};

const PDDAccountSettings: React.FC = () => {
  const [account,setAccount]=useState<PDDAccountConfig|null>(null);
  const [runtime,setRuntime]=useState<PDDAccountRuntime|null>(null); const [events,setEvents]=useState<PDDAccountEvent[]>([]); const [showEvents,setShowEvents]=useState(false);
  const [cookie,setCookie]=useState(''); const [showCookie,setShowCookie]=useState(false);
  const [busy,setBusy]=useState(false); const [message,setMessage]=useState<{ok:boolean;text:string}|null>(null);
  const load=()=>Promise.all([getPDDAccount(),getPDDAccountRuntime(),getPDDAccountEvents()]).then(([nextAccount,nextRuntime,nextEvents])=>{setAccount(nextAccount);setRuntime(nextRuntime);setEvents(nextEvents);}).catch(e=>setMessage({ok:false,text:e.message||'读取配置失败'}));
  useEffect(()=>{void load();},[]);
  if(!account)return <section className="ios-card rounded-xl p-6 bg-white text-sm text-gray-500">正在读取拼多多账号配置…</section>;
  const save=async()=>{setBusy(true);setMessage(null);try{const next=await savePDDAccount({name:account.name,site:account.site,cookie,default_address_id:account.default_address_id,user_agent:account.user_agent,enabled:account.enabled});setAccount(next);setCookie('');setMessage({ok:true,text:'拼多多账号配置已保存'});}catch(e){setMessage({ok:false,text:(e as Error).message||'保存失败'});}finally{setBusy(false)}};
  const verify=async()=>{setBusy(true);setMessage(null);try{const result=await verifyPDDAccount();setMessage({ok:true,text:result.message});load();}catch(e){setMessage({ok:false,text:(e as Error).message||'验证失败'});}finally{setBusy(false)}};
  const clear=async()=>{if(!window.confirm('确定清除当前拼多多账号配置？'))return;setBusy(true);try{await deletePDDAccount();setCookie('');load();setMessage({ok:true,text:'配置已清除'});}catch(e){setMessage({ok:false,text:(e as Error).message||'清除失败'});}finally{setBusy(false)}};
  const togglePaused=async()=>{setBusy(true);setMessage(null);try{account.enabled?await pausePDDAccount():await resumePDDAccount();await load();setMessage({ok:true,text:account.enabled?'拼多多账号已暂停':'拼多多账号已恢复，请先验证登录状态'});}catch(e){setMessage({ok:false,text:(e as Error).message||'操作失败'});}finally{setBusy(false)}};
  return <section className="space-y-4">
    <div><h3 className="text-lg font-extrabold text-gray-800">拼多多账号</h3><p className="text-xs text-gray-500 mt-1">当前先使用一个主账号，订单和操作记录已预留账号 ID。</p></div>
    <div className="ios-card rounded-xl p-6 bg-white space-y-4">
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <label className="space-y-2"><span className="text-sm font-bold">账号名称</span><input className="w-full ios-input px-4 py-3 rounded-xl" value={account.name} onChange={e=>setAccount({...account,name:e.target.value})}/></label>
        <label className="space-y-2"><span className="text-sm font-bold">拼多多站点</span><select className="w-full ios-input px-4 py-3 rounded-xl" value={account.site} onChange={e=>setAccount({...account,site:e.target.value as PDDAccountConfig['site']})}><option value="pinduoduo">mobile.pinduoduo.com（默认）</option><option value="yangkeduo">mobile.yangkeduo.com</option></select></label>
        <label className="space-y-2"><span className="text-sm font-bold">默认地址 ID</span><input className="w-full ios-input px-4 py-3 rounded-xl" value={account.default_address_id} onChange={e=>setAccount({...account,default_address_id:e.target.value})}/></label>
      </div>
      <label className="space-y-2 block"><span className="text-sm font-bold">Cookie（{account.cookie_domain}）</span><div className="relative"><input type={showCookie?'text':'password'} className="w-full ios-input px-4 py-3 pr-12 rounded-xl" value={cookie} placeholder={account.cookie_configured?'已安全保存；切换站点时必须重新采集':'粘贴包含 pdd_user_id 的完整 Cookie'} onChange={e=>setCookie(e.target.value)}/><button type="button" onClick={()=>setShowCookie(!showCookie)} className="absolute right-3 top-3 text-gray-400">{showCookie?<EyeOff className="w-5 h-5"/>:<Eye className="w-5 h-5"/>}</button></div></label>
      <p className="text-xs text-amber-700">Cookie、地址 ID 与浏览器会话按站点隔离；切换站点后请从对应地址页重新采集，系统不会混用另一域名的登录态。</p>
      <label className="space-y-2 block"><span className="text-sm font-bold">User-Agent（可选）</span><input className="w-full ios-input px-4 py-3 rounded-xl" value={account.user_agent} onChange={e=>setAccount({...account,user_agent:e.target.value})}/></label>
      <div className="flex flex-wrap items-center gap-3 text-sm"><span className="px-3 py-1.5 rounded-lg bg-gray-100">账号 ID：{account.pdd_uid||'保存 Cookie 后自动识别'}</span><span className={`px-3 py-1.5 rounded-lg ${account.credential_status==='valid'?'bg-green-50 text-green-700':'bg-amber-50 text-amber-700'}`}>状态：{account.credential_status}</span><label className="flex items-center gap-2"><input type="checkbox" checked={account.enabled} onChange={e=>setAccount({...account,enabled:e.target.checked})}/>启用</label></div>
      {message&&<div className={`p-3 rounded-xl text-sm ${message.ok?'bg-green-50 text-green-700':'bg-red-50 text-red-700'}`}>{message.text}</div>}
      {account.last_error&&<div className="text-sm text-red-600">{account.last_error}</div>}
      <div className="flex flex-wrap gap-3"><button disabled={busy} onClick={save} className="ios-btn-primary px-5 py-3 rounded-xl font-bold flex items-center gap-2"><Save className="w-4 h-4"/>保存账号</button><button disabled={busy||!account.configured} onClick={verify} className="px-5 py-3 rounded-xl bg-gray-100 font-bold flex items-center gap-2"><CheckCircle2 className="w-4 h-4"/>验证配置</button><button disabled={busy||!account.configured} onClick={clear} className="px-5 py-3 rounded-xl bg-red-50 text-red-600 font-bold flex items-center gap-2"><Trash2 className="w-4 h-4"/>清除</button><button onClick={load} className="p-3 rounded-xl bg-gray-100"><RefreshCw className="w-4 h-4"/></button></div>
    </div>
    {account.configured&&runtime&&<div className="ios-card rounded-xl p-6 bg-white space-y-4">
      <div className="flex items-center justify-between gap-3"><div className="flex items-center gap-2 font-extrabold text-gray-800"><Activity className="w-5 h-5"/>账号运行状态</div><button disabled={busy} onClick={togglePaused} className={`px-4 py-2 rounded-lg font-bold ${account.enabled?'bg-amber-50 text-amber-700':'bg-green-50 text-green-700'}`}>{account.enabled?'暂停账号':'恢复账号'}</button></div>
      <div className="grid grid-cols-2 lg:grid-cols-3 gap-3 text-sm">
        <div className="rounded-xl bg-gray-50 p-3"><div className="text-gray-500">账号状态</div><div className="font-bold mt-1">{statusText[runtime.status]||runtime.status}</div></div>
        <div className="rounded-xl bg-gray-50 p-3"><div className="text-gray-500">最近成功</div><div className="font-bold mt-1">{timeText(runtime.last_success_at)}</div></div>
        <div className="rounded-xl bg-gray-50 p-3"><div className="text-gray-500">最近失败</div><div className="font-bold mt-1 break-all">{runtime.last_error_type||'-'}</div></div>
        <div className="rounded-xl bg-gray-50 p-3"><div className="text-gray-500">连续失败</div><div className="font-bold mt-1">{runtime.consecutive_failures}</div></div>
        <div className="rounded-xl bg-gray-50 p-3"><div className="text-gray-500">当前任务</div><div className="font-bold mt-1">{runtime.current_task}</div></div>
        <div className="rounded-xl bg-gray-50 p-3"><div className="text-gray-500">今日操作</div><div className="font-bold mt-1">{runtime.today_operations}</div></div>
      </div>
      <button onClick={()=>setShowEvents(!showEvents)} className="text-sm font-bold text-blue-600">{showEvents?'收起最近日志':'查看最近日志'}</button>
      {showEvents&&<div className="divide-y rounded-xl border">{events.length===0?<div className="p-4 text-sm text-gray-500">暂无运行日志</div>:events.map(event=><div key={event.id} className="p-3 text-sm flex flex-wrap justify-between gap-2"><span><b>{operationText[event.operation]||event.operation}</b> · <span className={event.status==='success'?'text-green-600':'text-red-600'}>{event.status==='success'?'成功':event.error_type||'失败'}</span></span><span className="text-gray-400">{timeText(event.created_at)}</span></div>)}</div>}
    </div>}
  </section>;
};
export default PDDAccountSettings;
