import { useCallback, useEffect, useState } from 'react'
import { RefreshCw, Play, Pause, Trash2, AlertCircle, CheckCircle, X, Settings, Plus, Key } from 'lucide-react'
import { getTunnelStatus, manageTunnel, type TunnelStatus, type TunnelActionPayload } from '../services/api'

export default function Tunnels() {
  const [status, setStatus] = useState<TunnelStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [actionLoading, setActionLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [showCreateForm, setShowCreateForm] = useState(false)
  const [showAddPeerModal, setShowAddPeerModal] = useState(false)
  const [showRemovePeerModal, setShowRemovePeerModal] = useState<{ open: boolean; publicKey: string }>({
    open: false,
    publicKey: '',
  })
  const [peerForm, setPeerForm] = useState({
    peer_pub_key: '',
    peer_allowed_ips: '',
    peer_name: '',
    peer_endpoint: '',
  })

  const [createForm, setCreateForm] = useState({
    name: 'wg0',
    address: '10.250.250.1/24',
    listen_port: 51820,
    dns: '1.1.1.1',
  })

  const fetchData = useCallback(async () => {
    try {
      const res = await getTunnelStatus()
      setStatus(res.data.data || null)
      setError(null)
    } catch (err: any) {
      setError(err?.response?.data?.message || '获取隧道状态失败')
    } finally {
      setLoading(false)
      setRefreshing(false)
    }
  }, [])

  useEffect(() => { fetchData() }, [fetchData] )

  const runAction = async (payload: TunnelActionPayload) => {
    setActionLoading(true)
    setError(null)
    try {
      const res = await manageTunnel(payload)
      if (!res.data.success) {
        throw new Error(res.data.message || '操作失败')
      }
      await fetchData()
    } catch (err: any) {
      setError(err?.message || '操作失败')
    } finally {
      setActionLoading(false)
    }
  }

  const handleCreate = async () => {
    await runAction({
      action: 'create',
      address: createForm.address,
      listen_port: createForm.listen_port,
      dns: createForm.dns,
    })
    setShowCreateForm(false)
  }

  const handleEnable = () => runAction({ action: 'enable' })
  const handleDisable = () => runAction({ action: 'disable' })
  const handleDelete = () => runAction({ action: 'delete' })
  const handleResetKey = () => runAction({ action: 'reset_key' })
  const handleReload = () => runAction({ action: 'reload' })
  const handleSync = () => runAction({ action: 'sync' })
  const handleRefresh = () => { setRefreshing(true); fetchData(); }

  if (loading) {
    return (
      <div className="p-6 flex justify-center items-center min-h-screen">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-black dark:border-white"></div>
      </div>
    )
  }

  const tunnel = status

  return (
    <div className="p-6 space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-black dark:text-white">隧道管理</h1>
        <div className="flex items-center gap-2">
          <button
            onClick={handleRefresh}
            disabled={refreshing}
            className="p-2 rounded hover:bg-gray-100 dark:hover:bg-gray-800"
            title="刷新"
          >
            <RefreshCw className={`w-4 h-4 ${refreshing ? 'animate-spin' : ''}`} />
          </button>
          {tunnel && !tunnel.enabled && (
            <button
              onClick={() => setShowCreateForm(true)}
              className="px-3 py-1.5 text-sm rounded bg-black text-white hover:bg-gray-800 flex items-center gap-1"
            >
              <Settings className="w-4 h-4" /> 创建
            </button>
          )}
          {tunnel && tunnel.enabled && (
            <button
              onClick={() => setShowAddPeerModal(true)}
              className="px-3 py-1.5 text-sm rounded bg-blue-600 text-white hover:bg-blue-700 flex items-center gap-1"
            >
              <Plus className="w-4 h-4" /> 添加 Peer
            </button>
          )}
          {tunnel && tunnel.enabled && (
            <button
              onClick={handleResetKey}
              disabled={actionLoading}
              className="px-3 py-1.5 text-sm rounded bg-purple-600 text-white hover:bg-purple-700 flex items-center gap-1 disabled:opacity-50"
              title="重置服务端私钥与公钥"
            >
              <Key className="w-4 h-4" /> 重置密钥
            </button>
          )}
          {tunnel && tunnel.enabled && (
            <button
              onClick={handleReload}
              disabled={actionLoading}
              className="px-3 py-1.5 text-sm rounded bg-indigo-600 text-white hover:bg-indigo-700 flex items-center gap-1 disabled:opacity-50"
              title="重新加载 wg-quick 配置"
            >
              <RefreshCw className="w-4 h-4" /> 重载
            </button>
          )}
          {tunnel && tunnel.enabled && (
            <button
              onClick={handleSync}
              disabled={actionLoading}
              className="px-3 py-1.5 text-sm rounded bg-cyan-600 text-white hover:bg-cyan-700 flex items-center gap-1 disabled:opacity-50"
              title="把配置文件同步到运行中的接口"
            >
              <CheckCircle className="w-4 h-4" /> 同步
            </button>
          )}
        </div>
      </div>

      {/* Status Card */}
      {!tunnel ? (
        <div className="p-4 rounded bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800">
          <div className="flex items-center gap-3">
            <AlertCircle className="w-5 h-5 text-yellow-600" />
            <span className="text-yellow-800 dark:text-yellow-200">暂无 WireGuard 隧道配置</span>
          </div>
        </div>
      ) : (
        <div className="border rounded-lg p-4 bg-white dark:bg-gray-900">
          <div className="flex items-center justify-between mb-4">
            <div className="flex items-center gap-3">
              <h2 className="text-lg font-medium text-black dark:text-white">{tunnel.name}</h2>
              {tunnel.active ? (
                <CheckCircle className="w-5 h-5 text-green-600" />
              ) : (
                <X className="w-5 h-5 text-gray-400" />
              )}
            </div>
            <div className="flex items-center gap-2">
              {!tunnel.active && tunnel.enabled && (
                <button
                  onClick={handleEnable}
                  disabled={actionLoading}
                  className="px-3 py-1 text-sm rounded bg-green-600 text-white hover:bg-green-700 flex items-center gap-1"
                >
                  <Play className="w-4 h-4" /> 启用
                </button>
              )}
              {tunnel.active && (
                <button
                  onClick={handleDisable}
                  disabled={actionLoading}
                  className="px-3 py-1 text-sm rounded bg-orange-600 text-white hover:bg-orange-700 flex items-center gap-1"
                >
                  <Pause className="w-4 h-4" /> 停止
                </button>
              )}
              <button
                onClick={handleDelete}
                disabled={actionLoading || !tunnel.enabled}
                className="px-3 py-1 text-sm rounded bg-red-600 text-white hover:bg-red-700 flex items-center gap-1 disabled:opacity-50"
              >
                <Trash2 className="w-4 h-4" /> 删除
              </button>
            </div>
          </div>

          {/* Config */}
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 text-sm">
            <div>
              <label className="text-gray-500 dark:text-gray-400">监听端口</label>
              <p className="text-black dark:text-white font-mono">{tunnel.listen_port}</p>
            </div>
            <div>
              <label className="text-gray-500 dark:text-gray-400">地址</label>
              <p className="text-black dark:text-white font-mono">{tunnel.address}</p>
            </div>
            <div>
              <label className="text-gray-500 dark:text-gray-400">MTU</label>
              <p className="text-black dark:text-white font-mono">{tunnel.mtu}</p>
            </div>
            <div>
              <label className="text-gray-500 dark:text-gray-400">DNS</label>
              <p className="text-black dark:text-white font-mono">{tunnel.dns}</p>
            </div>
          </div>

          {/* Public key */}
          {tunnel.public_key && (
            <div className="mt-4 p-3 bg-gray-50 dark:bg-gray-800 rounded font-mono text-sm break-all">
              <span className="text-gray-600 dark:text-gray-400">公钥: </span>
              <span className="text-black dark:text-white">{tunnel.public_key}</span>
            </div>
          )}

          {/* Peers */}
          {tunnel.peers.length > 0 && (
            <div className="mt-4">
              <h3 className="text-sm font-medium text-black dark:text-white mb-2">Peer 列表 ({tunnel.peers.length})</h3>
              <div className="space-y-2">
                {tunnel.peers.map((peer) => (
                  <div key={peer.public_key} className="p-3 rounded border bg-gray-50 dark:bg-gray-800">
                    <div className="flex items-center justify-between">
                      <span className="font-mono text-xs text-black dark:text-white break-all">
                        {peer.public_key.slice(0, 16)}...{peer.public_key.slice(-8)}
                      </span>
                      <button
                        onClick={() => setShowRemovePeerModal({ open: true, publicKey: peer.public_key })}
                        className="text-red-600 hover:text-red-800"
                        title="移除 Peer"
                      >
                        <Trash2 className="w-4 h-4" />
                      </button>
                    </div>
                    {peer.endpoint && (
                      <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">Endpoint: {peer.endpoint}</p>
                    )}
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Status Info */}
          <div className="mt-4 grid grid-cols-2 md:grid-cols-4 gap-4 text-xs">
            <div>
              <span className="text-gray-500 dark:text-gray-400">配置路径</span>
              <p className="text-black dark:text-white">{tunnel.config_path}</p>
            </div>
            <div>
              <span className="text-gray-500 dark:text-gray-400">TUN 设备</span>
              <p className={tunnel.tun_available ? 'text-green-600' : 'text-red-600'}>
                {tunnel.tun_available ? '可用' : '不可用'}
              </p>
            </div>
            <div>
              <span className="text-gray-500 dark:text-gray-400">WireGuard</span>
              <p className={tunnel.wireguard_installed ? 'text-green-600' : 'text-red-600'}>
                {tunnel.wireguard_installed ? '已安装' : '未安装'}
              </p>
            </div>
            <div>
              <span className="text-gray-500 dark:text-gray-400">系统服务</span>
              <p className={tunnel.active ? 'text-green-600' : 'text-gray-400'}>
                {tunnel.enabled ? (tunnel.active ? '运行中' : '已启用') : '未启用'}
              </p>
            </div>
          </div>

          {error && (
            <div className="mt-4 p-3 rounded bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 flex items-center gap-2">
              <AlertCircle className="w-4 h-4 text-red-600" />
              <span className="text-red-800 dark:text-red-200 text-sm">{error}</span>
            </div>
          )}
        </div>
      )}

      {/* Create Form Modal */}
      {showCreateForm && (
        <div className="fixed inset-0 bg-black/50 dark:bg-black/60 flex items-center justify-center z-50">
          <div className="bg-white dark:bg-gray-900 rounded-lg p-6 w-full max-w-md">
            <h2 className="text-lg font-semibold text-black dark:text-white mb-4">创建 WireGuard 隧道</h2>
            <div className="space-y-4">
              <div>
                <label className="text-sm text-gray-600 dark:text-gray-300">内网地址</label>
                <input
                  type="text"
                  value={createForm.address}
                  onChange={(e) => setCreateForm({ ...createForm, address: e.target.value })}
                  className="mt-1 w-full px-3 py-2 border rounded dark:bg-gray-800 dark:border-gray-700"
                  placeholder="10.250.250.1/24"
                />
              </div>
              <div>
                <label className="text-sm text-gray-600 dark:text-gray-300">监听端口</label>
                <input
                  type="number"
                  value={createForm.listen_port}
                  onChange={(e) => setCreateForm({ ...createForm, listen_port: parseInt(e.target.value) || 51820 })}
                  className="mt-1 w-full px-3 py-2 border rounded dark:bg-gray-800 dark:border-gray-700"
                  min={1}
                  max={65535}
                />
              </div>
              <div>
                <label className="text-sm text-gray-600 dark:text-gray-300">DNS 服务器</label>
                <input
                  type="text"
                  value={createForm.dns}
                  onChange={(e) => setCreateForm({ ...createForm, dns: e.target.value })}
                  className="mt-1 w-full px-3 py-2 border rounded dark:bg-gray-800 dark:border-gray-700"
                  placeholder="1.1.1.1"
                />
              </div>
            </div>
            <div className="mt-6 flex justify-end gap-2">
              <button
                onClick={() => setShowCreateForm(false)}
                className="px-4 py-2 text-sm rounded border"
              >
                取消
              </button>
              <button
                onClick={handleCreate}
                disabled={actionLoading}
                className="px-4 py-2 text-sm rounded bg-black text-white hover:bg-gray-800"
              >
                创建
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Add Peer Modal */}
      {showAddPeerModal && (
        <div className="fixed inset-0 bg-black/50 dark:bg-black/60 flex items-center justify-center z-50">
          <div className="bg-white dark:bg-gray-900 rounded-lg p-6 w-full max-w-md">
            <h2 className="text-lg font-semibold text-black dark:text-white mb-4">添加 WireGuard Peer</h2>
            <div className="space-y-4">
              <div>
                <label className="text-sm text-gray-600 dark:text-gray-300">公钥</label>
                <input
                  type="text"
                  value={peerForm.peer_pub_key}
                  onChange={(e) => setPeerForm({ ...peerForm, peer_pub_key: e.target.value })}
                  className="mt-1 w-full px-3 py-2 border rounded dark:bg-gray-800 dark:border-gray-700"
                  placeholder="客户端公钥（Base64）"
                />
              </div>
              <div>
                <label className="text-sm text-gray-600 dark:text-gray-300">允许的 IP 网段</label>
                <input
                  type="text"
                  value={peerForm.peer_allowed_ips}
                  onChange={(e) => setPeerForm({ ...peerForm, peer_allowed_ips: e.target.value })}
                  className="mt-1 w-full px-3 py-2 border rounded dark:bg-gray-800 dark:border-gray-700"
                  placeholder="例：10.250.250.2/32"
                />
              </div>
              <div>
                <label className="text-sm text-gray-600 dark:text-gray-300">备注（可选）</label>
                <input
                  type="text"
                  value={peerForm.peer_name}
                  onChange={(e) => setPeerForm({ ...peerForm, peer_name: e.target.value })}
                  className="mt-1 w-full px-3 py-2 border rounded dark:bg-gray-800 dark:border-gray-700"
                  placeholder="客户端名称"
                />
              </div>
              <div>
                <label className="text-sm text-gray-600 dark:text-gray-300">端点（可选）</label>
                <input
                  type="text"
                  value={peerForm.peer_endpoint}
                  onChange={(e) => setPeerForm({ ...peerForm, peer_endpoint: e.target.value })}
                  className="mt-1 w-full px-3 py-2 border rounded dark:bg-gray-800 dark:border-gray-700"
                  placeholder="例：1.2.3.4:51820"
                />
              </div>
            </div>
            <div className="mt-6 flex justify-end gap-2">
              <button
                onClick={() => setShowAddPeerModal(false)}
                className="px-4 py-2 text-sm rounded border"
              >
                取消
              </button>
              <button
                onClick={() => {
                  runAction({
                    action: 'add_peer',
                    peer_pub_key: peerForm.peer_pub_key,
                    peer_allowed_ips: peerForm.peer_allowed_ips,
                    peer_name: peerForm.peer_name,
                    peer_endpoint: peerForm.peer_endpoint,
                  })
                  setPeerForm({ peer_pub_key: '', peer_allowed_ips: '', peer_name: '', peer_endpoint: '' })
                  setShowAddPeerModal(false)
                }}
                disabled={actionLoading || !peerForm.peer_pub_key || !peerForm.peer_allowed_ips}
                className="px-4 py-2 text-sm rounded bg-blue-600 text-white hover:bg-blue-700 disabled:opacity-50"
              >
                添加
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Remove Peer Modal */}
      {showRemovePeerModal.open && (
        <div className="fixed inset-0 bg-black/50 dark:bg-black/60 flex items-center justify-center z-50">
          <div className="bg-white dark:bg-gray-900 rounded-lg p-6 w-full max-w-sm">
            <h2 className="text-lg font-semibold text-black dark:text-white mb-2">移除 Peer</h2>
            <p className="text-sm text-gray-600 dark:text-gray-300 mb-4">
              确定移除公钥为 <span className="font-mono">{showRemovePeerModal.publicKey.slice(0, 16)}...{showRemovePeerModal.publicKey.slice(-8)}</span> 的 Peer 吗？
            </p>
            <div className="flex justify-end gap-2">
              <button
                onClick={() => setShowRemovePeerModal({ open: false, publicKey: '' })}
                className="px-4 py-2 text-sm rounded border"
              >
                取消
              </button>
              <button
                onClick={() => {
                  runAction({ action: 'remove_peer', peer_pub_key: showRemovePeerModal.publicKey })
                  setShowRemovePeerModal({ open: false, publicKey: '' })
                }}
                disabled={actionLoading}
                className="px-4 py-2 text-sm rounded bg-red-600 text-white hover:bg-red-700 disabled:opacity-50"
              >
                移除
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}