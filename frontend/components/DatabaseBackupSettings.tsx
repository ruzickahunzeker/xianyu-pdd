import React, {useEffect, useState} from 'react';
import {Archive, CheckCircle2, Download, HardDrive, RefreshCw, ShieldAlert} from 'lucide-react';
import {createDatabaseBackup, databaseBackupDownloadURL, DatabaseBackup, getDatabaseBackups} from '../services/api';
import {SystemSettings} from '../types';

interface Props { settings: SystemSettings; onChange: (settings: SystemSettings) => void }

const formatSize = (bytes: number) => bytes < 1024 * 1024
  ? `${Math.max(1, Math.round(bytes / 1024))} KB`
  : `${(bytes / 1024 / 1024).toFixed(1)} MB`;

const DatabaseBackupSettings: React.FC<Props> = ({settings, onChange}) => {
  const [items, setItems] = useState<DatabaseBackup[]>([]);
  const [dialect, setDialect] = useState('');
  const [loading, setLoading] = useState(false);
  const [creating, setCreating] = useState(false);
  const [message, setMessage] = useState('');

  const load = async () => {
    setLoading(true);
    setMessage('');
    try {
      const result = await getDatabaseBackups();
      setItems(result.backups || []);
      setDialect(result.dialect || '');
    } catch (error) {
      setMessage((error as Error).message || '读取备份失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void load(); }, []);

  const create = async () => {
    setCreating(true);
    setMessage('');
    try {
      const entry = await createDatabaseBackup();
      setMessage(`备份完成：${formatSize(entry.size_bytes)}，SKU 映射 ${entry.mapping_rows} 条`);
      await load();
    } catch (error) {
      setMessage((error as Error).message || '创建备份失败');
    } finally {
      setCreating(false);
    }
  };

  return (
    <section className="space-y-4">
      <h3 className="text-lg font-extrabold text-gray-800 flex items-center gap-2">
        <div className="p-1.5 rounded-lg bg-emerald-600 text-white"><HardDrive className="w-4 h-4" /></div>
        数据库备份
      </h3>
      <div className="ios-card rounded-xl p-6 bg-white space-y-5">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div><div className="text-sm font-bold text-gray-900">当前数据库：{dialect || '读取中'}</div><div className="mt-1 text-xs text-gray-500">备份包含完整数据库、校验清单和 SKU 映射 CSV</div></div>
          <button type="button" onClick={create} disabled={creating} className="px-4 py-2.5 rounded-xl bg-emerald-600 hover:bg-emerald-700 text-white font-bold text-sm flex items-center gap-2 disabled:opacity-50">
            <Archive className="w-4 h-4" />{creating ? '正在备份…' : '立即备份'}
          </button>
        </div>

        <label className="flex items-center justify-between gap-4 rounded-xl bg-gray-50 p-4">
          <span><span className="block text-sm font-bold text-gray-800">自动备份</span><span className="text-xs text-gray-500">按间隔执行，超过保留份数后只清理本系统备份目录中的旧文件</span></span>
          <input type="checkbox" checked={String(settings.backup_enabled ?? 'false') === 'true'} onChange={e => onChange({...settings, backup_enabled: e.target.checked})} className="h-5 w-5" />
        </label>
        <div className="grid grid-cols-2 gap-4">
          <label className="text-sm font-bold text-gray-700">间隔（小时）<input type="number" min="1" max="720" value={Number(settings.backup_interval_hours || 24)} onChange={e => onChange({...settings, backup_interval_hours: Number(e.target.value)})} className="mt-2 w-full ios-input px-4 py-3 rounded-xl" /></label>
          <label className="text-sm font-bold text-gray-700">保留份数<input type="number" min="1" max="365" value={Number(settings.backup_retention_count || 14)} onChange={e => onChange({...settings, backup_retention_count: Number(e.target.value)})} className="mt-2 w-full ios-input px-4 py-3 rounded-xl" /></label>
        </div>

        <div className="rounded-xl bg-amber-50 px-4 py-3 text-xs text-amber-800 flex gap-2"><ShieldAlert className="w-4 h-4 shrink-0" /><span>数据库中的 Cookie 等敏感数据依赖 XIANYU_DATA_KEY 解密。请另外安全保存该密钥；系统不会把密钥明文写进备份。</span></div>
        {message && <div className="text-sm text-gray-700">{message}</div>}

        <div className="border-t border-gray-100 pt-4 space-y-2">
          <div className="flex items-center justify-between"><span className="text-sm font-bold text-gray-800">最近备份</span><button type="button" onClick={load} disabled={loading} className="p-2 text-gray-500 hover:text-gray-900"><RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} /></button></div>
          {items.length === 0 ? <div className="py-4 text-center text-sm text-gray-400">暂无备份</div> : items.slice(0, 8).map(item => (
            <div key={item.id} className="flex items-center justify-between gap-3 rounded-xl border border-gray-100 p-3">
              <div className="min-w-0"><div className="flex items-center gap-2 text-sm font-bold text-gray-800"><CheckCircle2 className="w-4 h-4 text-emerald-600" />{new Date(item.created_at * 1000).toLocaleString()}</div><div className="mt-1 truncate text-xs text-gray-500">{formatSize(item.size_bytes)} · 映射 {item.mapping_rows} 条 · SHA-256 {item.sha256.slice(0, 12)}…</div></div>
              <a href={databaseBackupDownloadURL(item.id)} className="p-2 rounded-lg bg-gray-100 hover:bg-gray-200 text-gray-700" title="下载备份"><Download className="w-4 h-4" /></a>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
};

export default DatabaseBackupSettings;
