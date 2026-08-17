import React, { useEffect, useState } from 'react';
import { Check, Copy, KeyRound, Plus, RefreshCw, Trash2 } from 'lucide-react';
import {
  createFulfillmentAPIKey,
  FulfillmentAPIKey,
  getFulfillmentAPIKeys,
  revokeFulfillmentAPIKey,
} from '../services/api';

const formatTime = (timestamp: number): string => {
  if (!timestamp) return '尚未使用';
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false,
  }).format(new Date(timestamp * 1000));
};

const FulfillmentKeyManager: React.FC = () => {
  const [keys, setKeys] = useState<FulfillmentAPIKey[]>([]);
  const [name, setName] = useState('');
  const [newKey, setNewKey] = useState('');
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [revoking, setRevoking] = useState('');
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState('');

  const loadKeys = async () => {
    setLoading(true);
    setError('');
    try {
      setKeys(await getFulfillmentAPIKeys());
    } catch (err) {
      setError((err as Error).message || '读取履约密钥失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void loadKeys(); }, []);

  const createKey = async (event: React.FormEvent) => {
    event.preventDefault();
    const keyName = name.trim();
    if (!keyName) return;
    setCreating(true);
    setError('');
    try {
      const created = await createFulfillmentAPIKey(keyName);
      setNewKey(created.api_key);
      setName('');
      setCopied(false);
      await loadKeys();
    } catch (err) {
      setError((err as Error).message || '创建履约密钥失败');
    } finally {
      setCreating(false);
    }
  };

  const copyKey = async () => {
    try {
      await navigator.clipboard.writeText(newKey);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1800);
    } catch {
      setError('自动复制失败，请选中密钥后手动复制');
    }
  };

  const revokeKey = async (key: FulfillmentAPIKey) => {
    if (!window.confirm(`确定吊销履约密钥“${key.name}”吗？使用该密钥的脚本将立即无法访问。`)) return;
    setRevoking(key.id);
    setError('');
    try {
      await revokeFulfillmentAPIKey(key.id);
      setKeys(current => current.filter(item => item.id !== key.id));
    } catch (err) {
      setError((err as Error).message || '吊销履约密钥失败');
    } finally {
      setRevoking('');
    }
  };

  return (
    <section className="space-y-4">
      <h3 className="text-lg font-extrabold text-gray-800 flex items-center gap-2">
        <div className="p-1.5 rounded-lg bg-blue-600 text-white"><KeyRound className="w-4 h-4" /></div>
        履约密钥管理
      </h3>
      <div className="ios-card rounded-xl p-6 bg-white space-y-5">
        <p className="text-sm text-gray-500">供订单履约脚本调用统一 API。密钥只在创建时显示一次，请及时复制保存。</p>

        <form onSubmit={createKey} className="flex flex-col sm:flex-row gap-2">
          <input
            value={name}
            onChange={event => setName(event.target.value)}
            maxLength={100}
            className="flex-1 ios-input px-4 py-3 rounded-xl text-sm"
            placeholder="例如：本地履约脚本"
          />
          <button type="submit" disabled={creating || !name.trim()} className="px-4 py-3 rounded-xl bg-blue-600 hover:bg-blue-700 text-white font-bold text-sm flex items-center justify-center gap-2 disabled:opacity-40">
            <Plus className="w-4 h-4" />{creating ? '创建中...' : '创建密钥'}
          </button>
        </form>

        {newKey && (
          <div className="rounded-xl border border-green-200 bg-green-50 p-4 space-y-3">
            <div className="text-sm font-bold text-green-800">密钥创建成功，请立即复制</div>
            <div className="flex gap-2">
              <input readOnly value={newKey} onFocus={event => event.currentTarget.select()} className="min-w-0 flex-1 rounded-lg border border-green-200 bg-white px-3 py-2 font-mono text-xs text-gray-800" />
              <button type="button" onClick={copyKey} className="px-3 py-2 rounded-lg bg-green-600 hover:bg-green-700 text-white text-sm font-bold flex items-center gap-1.5">
                {copied ? <Check className="w-4 h-4" /> : <Copy className="w-4 h-4" />}{copied ? '已复制' : '复制'}
              </button>
            </div>
          </div>
        )}

        {error && <div className="rounded-xl bg-red-50 px-4 py-3 text-sm text-red-700">{error}</div>}

        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <span className="text-sm font-bold text-gray-800">现有密钥</span>
            <button type="button" onClick={loadKeys} disabled={loading} className="p-2 rounded-lg text-gray-500 hover:bg-gray-100" title="刷新密钥列表">
              <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
            </button>
          </div>
          {!loading && keys.length === 0 ? (
            <div className="rounded-xl bg-gray-50 py-7 text-center text-sm text-gray-400">尚未创建履约密钥</div>
          ) : keys.map(key => (
            <div key={key.id} className="rounded-xl border border-gray-100 p-4 flex items-start justify-between gap-4">
              <div className="min-w-0 space-y-1">
                <div className="font-bold text-sm text-gray-900 truncate">{key.name}</div>
                <div className="text-xs text-gray-500">最近使用：{formatTime(key.last_used_at)}</div>
                <div className="text-xs text-gray-400">创建时间：{formatTime(key.created_at)}</div>
              </div>
              <button type="button" onClick={() => revokeKey(key)} disabled={revoking === key.id} className="shrink-0 p-2 rounded-lg text-red-500 hover:bg-red-50 disabled:opacity-40" title="吊销密钥">
                <Trash2 className="w-4 h-4" />
              </button>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
};

export default FulfillmentKeyManager;
