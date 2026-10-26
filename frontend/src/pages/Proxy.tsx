import { useState, useEffect, useCallback, type ReactNode } from 'react'
import { Loader2, Plus, Trash2, Power, Globe, Shield, RefreshCw, X } from 'lucide-react'
import { useLanguage } from '../contexts/LanguageContext'
import {
  getProxyRules,
  createProxyRule,
  updateProxyRule,
  deleteProxyRule,
  getContainers,
  type ProxyRule,
  type ProxyRuleCreate,
  type Container,
} from '../services/api'

function Modal({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div className="flex max-h-[88vh] w-full flex-col overflow-hidden rounded-lg border border-gray-200 bg-white shadow-xl max-w-lg dark:border-gray-700 dark:bg-gray-900" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between gap-3 border-b border-gray-200 px-5 py-4 dark:border-gray-700">
          <div className="text-base font-semibold">{title}</div>
          <button onClick={onClose} className="rounded p-1 text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-800">
            <X className="h-5 w-5" />
          </button>
        </div>
        <div className="overflow-y-auto p-4">{children}</div>
      </div>
    </div>
  )
}

export default function Proxy() {
  const { t } = useLanguage()
  const [rules, setRules] = useState<ProxyRule[]>([])
  const [containers, setContainers] = useState<Container[]>([])
  const [loading, setLoading] = useState(true)
  const [actionLoading, setActionLoading] = useState(false)
  const [showCreate, setShowCreate] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<ProxyRule | null>(null)

  const emptyForm: ProxyRuleCreate & { name: string } = {
    domain: '',
    path: '',
    target_container_id: 0,
    target_port: 80,
    name: '',
    tls: true,
  }
  const [form, setForm] = useState(emptyForm)

  const containerName = (id: number) => containers.find((c) => c.id === id)?.name || `#${id}`

  const loadData = useCallback(async () => {
    setLoading(true)
    try {
      const [rulesRes, containersRes] = await Promise.all([getProxyRules(), getContainers()])
      setRules((rulesRes.data.data as ProxyRule[]) || [])
      setContainers((containersRes.data.data as Container[]) || [])
    } catch {
      /* network */
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    loadData()
  }, [loadData])

  const handleCreate = async () => {
    if (!form.domain.trim() || !form.target_container_id || !form.target_port) return
    setActionLoading(true)
    try {
      await createProxyRule(form)
      setShowCreate(false)
      setForm(emptyForm)
      await loadData()
    } catch {
      /* network */
    } finally {
      setActionLoading(false)
    }
  }

  const handleToggle = async (rule: ProxyRule) => {
    setActionLoading(true)
    try {
      await updateProxyRule(rule.id, { enabled: !rule.enabled })
      await loadData()
    } catch {
      /* network */
    } finally {
      setActionLoading(false)
    }
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    setActionLoading(true)
    try {
      await deleteProxyRule(deleteTarget.id)
      setDeleteTarget(null)
      await loadData()
    } catch {
      /* network */
    } finally {
      setActionLoading(false)
    }
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold flex items-center gap-2">
            <Globe className="w-5 h-5" />
            {t('反向代理')}
          </h1>
          <p className="text-sm text-gray-500 mt-1">
            {t('基于 Caddy 自动签发 Let\'s Encrypt 证书，将域名反代到 LXC 容器端口')}
          </p>
        </div>
        <div className="flex gap-2">
          <button
            onClick={loadData}
            className="p-2 rounded-md hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors"
            title={t('刷新')}
          >
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
          </button>
          <button
            onClick={() => setShowCreate(true)}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-md bg-black text-white dark:bg-white dark:text-black text-sm hover:opacity-80 transition-opacity"
          >
            <Plus className="w-4 h-4" />
            {t('添加规则')}
          </button>
        </div>
      </div>

      {/* List */}
      {loading ? (
        <div className="flex justify-center py-12">
          <Loader2 className="w-6 h-6 animate-spin text-gray-400" />
        </div>
      ) : rules.length === 0 ? (
        <div className="text-center py-16 text-gray-400">
          <Globe className="w-12 h-12 mx-auto mb-3 opacity-40" />
          <p>{t('暂无反代规则')}</p>
          <p className="text-xs mt-1">{t('点击「添加规则」创建第一条反向代理')}</p>
        </div>
      ) : (
        <div className="space-y-3">
          {rules.map((rule) => (
            <div
              key={rule.id}
              className="flex items-center justify-between p-4 rounded-lg border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900"
            >
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 flex-wrap">
                  <span className="font-medium font-mono truncate">{rule.domain}</span>
                  {rule.path && (
                    <span className="text-xs text-gray-400 font-mono">{rule.path}</span>
                  )}
                  {rule.tls && (
                    <span className="flex items-center gap-0.5 text-xs px-1.5 py-0.5 rounded bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400">
                      <Shield className="w-3 h-3" /> HTTPS
                    </span>
                  )}
                  <span
                    className={`text-xs px-1.5 py-0.5 rounded ${
                      rule.enabled
                        ? 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400'
                        : 'bg-gray-100 text-gray-500 dark:bg-gray-800 dark:text-gray-400'
                    }`}
                  >
                    {rule.enabled ? t('已启用') : t('已禁用')}
                  </span>
                </div>
                <p className="text-sm text-gray-500 mt-1">
                  {t('容器')}: {rule.target_container || containerName(rule.target_container_id)} → :{rule.target_port}
                  {rule.name ? ` · ${rule.name}` : ''}
                </p>
              </div>
              <div className="flex items-center gap-1 ml-3">
                <button
                  onClick={() => handleToggle(rule)}
                  disabled={actionLoading}
                  className="p-1.5 rounded hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors"
                  title={rule.enabled ? t('禁用') : t('启用')}
                >
                  <Power className={`w-4 h-4 ${rule.enabled ? 'text-green-600' : 'text-gray-400'}`} />
                </button>
                <button
                  onClick={() => setDeleteTarget(rule)}
                  disabled={actionLoading}
                  className="p-1.5 rounded hover:bg-red-50 dark:hover:bg-red-900/20 text-gray-400 hover:text-red-500 transition-colors"
                  title={t('删除')}
                >
                  <Trash2 className="w-4 h-4" />
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Create Modal */}
      {showCreate && (
        <Modal title={t('添加反代规则')} onClose={() => setShowCreate(false)}>
          <div className="space-y-4">
            <div>
              <label className="block text-sm font-medium mb-1">{t('域名')} *</label>
              <input
                value={form.domain}
                onChange={(e) => setForm({ ...form, domain: e.target.value })}
                placeholder="app.example.com"
                className="w-full px-3 py-2 rounded-md border border-gray-300 dark:border-gray-600 bg-transparent text-sm font-mono"
              />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-sm font-medium mb-1">{t('目标容器')} *</label>
                <select
                  value={form.target_container_id || ''}
                  onChange={(e) => setForm({ ...form, target_container_id: Number(e.target.value) })}
                  className="w-full px-3 py-2 rounded-md border border-gray-300 dark:border-gray-600 bg-transparent text-sm"
                >
                  <option value="">{t('选择容器')}</option>
                  {containers.map((c) => (
                    <option key={c.id} value={c.id}>
                      {c.name} (#{c.id})
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <label className="block text-sm font-medium mb-1">{t('容器端口')} *</label>
                <input
                  type="number"
                  value={form.target_port}
                  onChange={(e) => setForm({ ...form, target_port: Number(e.target.value) })}
                  className="w-full px-3 py-2 rounded-md border border-gray-300 dark:border-gray-600 bg-transparent text-sm"
                />
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium mb-1">{t('路径')}（{t('可选')}）</label>
              <input
                value={form.path}
                onChange={(e) => setForm({ ...form, path: e.target.value })}
                placeholder="/api"
                className="w-full px-3 py-2 rounded-md border border-gray-300 dark:border-gray-600 bg-transparent text-sm font-mono"
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-1">{t('备注')}（{t('可选')}）</label>
              <input
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                className="w-full px-3 py-2 rounded-md border border-gray-300 dark:border-gray-600 bg-transparent text-sm"
              />
            </div>
            <label className="flex items-center gap-2 cursor-pointer">
              <input
                type="checkbox"
                checked={form.tls}
                onChange={(e) => setForm({ ...form, tls: e.target.checked })}
                className="w-4 h-4"
              />
              <span className="text-sm flex items-center gap-1">
                <Shield className="w-3.5 h-3.5" /> {t('启用 HTTPS')}（Let's Encrypt {t('自动签发')}）
              </span>
            </label>
          </div>
          <div className="flex justify-end gap-2 mt-5">
            <button
              onClick={() => setShowCreate(false)}
              className="px-3 py-1.5 rounded-md text-sm hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors"
            >
              {t('取消')}
            </button>
            <button
              onClick={handleCreate}
              disabled={actionLoading || !form.domain.trim() || !form.target_container_id}
              className="px-3 py-1.5 rounded-md bg-black text-white dark:bg-white dark:text-black text-sm hover:opacity-80 transition-opacity disabled:opacity-40"
            >
              {actionLoading ? <Loader2 className="w-4 h-4 animate-spin" /> : t('创建')}
            </button>
          </div>
        </Modal>
      )}

      {/* Delete confirm */}
      {deleteTarget && (
        <Modal title={t('删除规则')} onClose={() => setDeleteTarget(null)}>
          <div className="w-[360px]">
            <p className="text-sm">
              {t('确认删除')} <span className="font-mono font-medium">{deleteTarget.domain}</span> {t('的反代规则？')}
            </p>
            <p className="text-xs text-gray-400 mt-2">{t('删除后 Caddy 将停止为该域名提供反代服务')}</p>
          </div>
          <div className="flex justify-end gap-2 mt-5">
            <button
              onClick={() => setDeleteTarget(null)}
              className="px-3 py-1.5 rounded-md text-sm hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors"
            >
              {t('取消')}
            </button>
            <button
              onClick={handleDelete}
              disabled={actionLoading}
              className="px-3 py-1.5 rounded-md bg-red-500 text-white text-sm hover:bg-red-600 transition-colors disabled:opacity-40"
            >
              {actionLoading ? <Loader2 className="w-4 h-4 animate-spin" /> : t('删除')}
            </button>
          </div>
        </Modal>
      )}
    </div>
  )
}
