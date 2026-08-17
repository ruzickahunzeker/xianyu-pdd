import React, {useEffect, useState} from 'react';
import {CheckCircle2, Eye, EyeOff, RefreshCw, Save, Trash2} from 'lucide-react';
import {deletePDDAccount, getPDDAccount, savePDDAccount, verifyPDDAccount} from '../services/api';
import type {PDDAccountConfig} from '../types';

const PDDAccountSettings: React.FC = () => {
  const [account,setAccount]=useState<PDDAccountConfig|null>(null);
  const [cookie,setCookie]=useState(''); const [showCookie,setShowCookie]=useState(false);
  const [busy,setBusy]=useState(false); const [message,setMessage]=useState<{ok:boolean;text:string}|null>(null);
  const load=()=>getPDDAccount().then(setAccount).catch(e=>setMessage({ok:false,text:e.message||'读取配置失败'}));
  useEffect(()=>{void load();},[]);
  if(!account)return <section className="ios-card rounded-xl p-6 bg-white text-sm text-gray-500">正在读取拼多多账号配置…</section>;
  const save=async()=>{setBusy(true);setMessage(null);try{const next=await savePDDAccount({name:account.name,cookie,default_address_id:account.default_address_id,user_agent:account.user_agent,enabled:account.enabled});setAccount(next);setCookie('');setMessage({ok:true,text:'拼多多账号配置已保存'});}catch(e){setMessage({ok:false,text:(e as Error).message||'保存失败'});}finally{setBusy(false)}};
  const verify=async()=>{setBusy(true);setMessage(null);try{const result=await verifyPDDAccount();setMessage({ok:true,text:result.message});load();}catch(e){setMessage({ok:false,text:(e as Error).message||'验证失败'});}finally{setBusy(false)}};
  const clear=async()=>{if(!window.confirm('确定清除当前拼多多账号配置？'))return;setBusy(true);try{await deletePDDAccount();setCookie('');load();setMessage({ok:true,text:'配置已清除'});}catch(e){setMessage({ok:false,text:(e as Error).message||'清除失败'});}finally{setBusy(false)}};
  return <section className="space-y-4">
    <div><h3 className="text-lg font-extrabold text-gray-800">拼多多账号</h3><p className="text-xs text-gray-500 mt-1">当前先使用一个主账号，订单和操作记录已预留账号 ID。</p></div>
    <div className="ios-card rounded-xl p-6 bg-white space-y-4">
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <label className="space-y-2"><span className="text-sm font-bold">账号名称</span><input className="w-full ios-input px-4 py-3 rounded-xl" value={account.name} onChange={e=>setAccount({...account,name:e.target.value})}/></label>
        <label className="space-y-2"><span className="text-sm font-bold">默认地址 ID</span><input className="w-full ios-input px-4 py-3 rounded-xl" value={account.default_address_id} onChange={e=>setAccount({...account,default_address_id:e.target.value})}/></label>
      </div>
      <label className="space-y-2 block"><span className="text-sm font-bold">Cookie</span><div className="relative"><input type={showCookie?'text':'password'} className="w-full ios-input px-4 py-3 pr-12 rounded-xl" value={cookie} placeholder={account.cookie_configured?'已安全保存；留空表示不修改':'粘贴包含 pdd_user_id 的完整 Cookie'} onChange={e=>setCookie(e.target.value)}/><button type="button" onClick={()=>setShowCookie(!showCookie)} className="absolute right-3 top-3 text-gray-400">{showCookie?<EyeOff className="w-5 h-5"/>:<Eye className="w-5 h-5"/>}</button></div></label>
      <label className="space-y-2 block"><span className="text-sm font-bold">User-Agent（可选）</span><input className="w-full ios-input px-4 py-3 rounded-xl" value={account.user_agent} onChange={e=>setAccount({...account,user_agent:e.target.value})}/></label>
      <div className="flex flex-wrap items-center gap-3 text-sm"><span className="px-3 py-1.5 rounded-lg bg-gray-100">账号 ID：{account.pdd_uid||'保存 Cookie 后自动识别'}</span><span className={`px-3 py-1.5 rounded-lg ${account.credential_status==='valid'?'bg-green-50 text-green-700':'bg-amber-50 text-amber-700'}`}>状态：{account.credential_status}</span><label className="flex items-center gap-2"><input type="checkbox" checked={account.enabled} onChange={e=>setAccount({...account,enabled:e.target.checked})}/>启用</label></div>
      {message&&<div className={`p-3 rounded-xl text-sm ${message.ok?'bg-green-50 text-green-700':'bg-red-50 text-red-700'}`}>{message.text}</div>}
      {account.last_error&&<div className="text-sm text-red-600">{account.last_error}</div>}
      <div className="flex flex-wrap gap-3"><button disabled={busy} onClick={save} className="ios-btn-primary px-5 py-3 rounded-xl font-bold flex items-center gap-2"><Save className="w-4 h-4"/>保存账号</button><button disabled={busy||!account.configured} onClick={verify} className="px-5 py-3 rounded-xl bg-gray-100 font-bold flex items-center gap-2"><CheckCircle2 className="w-4 h-4"/>验证配置</button><button disabled={busy||!account.configured} onClick={clear} className="px-5 py-3 rounded-xl bg-red-50 text-red-600 font-bold flex items-center gap-2"><Trash2 className="w-4 h-4"/>清除</button><button onClick={load} className="p-3 rounded-xl bg-gray-100"><RefreshCw className="w-4 h-4"/></button></div>
    </div>
  </section>;
};
export default PDDAccountSettings;
