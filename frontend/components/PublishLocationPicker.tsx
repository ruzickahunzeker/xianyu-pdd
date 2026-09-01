import React, {useEffect, useState} from 'react';
import {LocateFixed, Search} from 'lucide-react';
import type {PublishLocation} from '../services/api';
import {getPublishLocations, searchPublishLocations} from '../services/amapLocation';

interface Props {
  accountID: string;
  value: PublishLocation | null;
  onChange: (location: PublishLocation | null) => void;
  compact?: boolean;
  title?: string;
}

const storageKey = (accountID: string) => `xianyu.publish-location.${accountID}`;
const label = (item: PublishLocation) => [item.province, item.city, item.area, item.poi_name].filter(Boolean).join(' ');

export const PublishLocationPicker: React.FC<Props> = ({accountID, value, onChange, compact = false, title = '实物商品发货地'}) => {
  const [keyword, setKeyword] = useState('');
  const [results, setResults] = useState<PublishLocation[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    setResults([]); setKeyword(''); setError('');
    if (!accountID) return onChange(null);
    try {
      const saved = window.localStorage.getItem(storageKey(accountID));
      if (saved) onChange(JSON.parse(saved));
    } catch { window.localStorage.removeItem(storageKey(accountID)); }
  }, [accountID]);

  const choose = (location: PublishLocation) => {
    onChange(location);
    window.localStorage.setItem(storageKey(accountID), JSON.stringify(location));
  };
  const searchLocation = async () => {
    if (!accountID) return setError('请先选择发布账号');
    setBusy(true); setError('');
    try {
      const locations = await searchPublishLocations(keyword);
      setResults(locations);
      if (!locations.length) setError('未找到地点，请尝试输入更完整的省市区或地标');
    } catch (reason: any) { setError(reason?.message || '搜索发货地失败'); }
    finally { setBusy(false); }
  };
  const locate = () => {
    if (!accountID) return setError('请先选择发布账号');
    if (!navigator.geolocation) return setError('当前浏览器不支持定位，请使用搜索');
    setBusy(true); setError('');
    navigator.geolocation.getCurrentPosition(async position => {
      try {
        const locations = await getPublishLocations(position.coords.longitude, position.coords.latitude);
        setResults(locations);
        if (locations[0]) choose(locations[0]); else setError('当前位置附近没有可用地点');
      } catch (reason: any) { setError(reason?.message || '获取发货地失败'); }
      finally { setBusy(false); }
    }, reason => {
      setBusy(false);
      setError(reason.code === reason.PERMISSION_DENIED ? '定位权限被拒绝，可直接搜索发货地' : '无法获取当前位置，可直接搜索');
    }, {enableHighAccuracy: true, timeout: 15_000, maximumAge: 60_000});
  };

  return <div className={`rounded-xl border border-sky-200 bg-sky-50 ${compact ? 'p-3' : 'p-4'} space-y-3`}>
    <div><div className="text-sm font-extrabold text-gray-900">{title}</div><p className="mt-1 text-xs text-sky-800">可搜索省市区、商圈或地标；选择结果后才会提交。</p></div>
    <form className="flex gap-2" onSubmit={event => {event.preventDefault(); void searchLocation();}}>
      <input aria-label="搜索发货地" className="min-w-0 flex-1 rounded-lg border bg-white px-3 py-2 text-sm" value={keyword} onChange={event=>setKeyword(event.target.value)} placeholder="例：广东 深圳 福田 / 华强北"/>
      <button type="submit" disabled={busy||!accountID} className="flex items-center gap-1 rounded-lg border bg-white px-3 py-2 text-xs font-bold disabled:opacity-50"><Search className="h-4 w-4"/>搜索</button>
      <button type="button" disabled={busy||!accountID} onClick={locate} className="flex items-center gap-1 rounded-lg bg-sky-600 px-3 py-2 text-xs font-bold text-white disabled:opacity-50"><LocateFixed className="h-4 w-4"/>{busy?'查询中…':'当前位置'}</button>
    </form>
    {value && <div className="rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm"><span className="font-bold">已选：</span>{label(value)}</div>}
    {results.length > 0 && <select aria-label="发货地搜索结果" className="w-full rounded-lg border bg-white p-3 text-sm" value={value ? `${value.division_id}:${value.poi_id}` : ''} onChange={event=>{const item=results.find(row=>`${row.division_id}:${row.poi_id}`===event.target.value);if(item)choose(item);}}><option value="">请选择搜索结果</option>{results.map((item,index)=><option key={`${item.division_id}-${item.poi_id}-${index}`} value={`${item.division_id}:${item.poi_id}`}>{label(item)}</option>)}</select>}
    {error && <p className="text-xs font-bold text-red-600">{error}</p>}
  </div>;
};
