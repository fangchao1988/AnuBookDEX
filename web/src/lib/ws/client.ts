// WebSocket 客户端：对接后端 internal/dex/ws（cmd 订阅协议 + token 鉴权）
//
// 服务端推送无信封，客户端按 payload 内容推断频道：
//
//	{type:"market.orderBook", pairCode} -> depth.{pairCode}
//	{type:"market.candles", pairCode, interval} -> kline.{pairCode}.{interval}
//	{type:"market.fills", pairCode} -> trade.{pairCode}
//	{type:"market.ticker", pairCode} -> ticker.{pairCode}
//
// 重连策略：指数退避 1s -> 30s，重连成功后自动重订阅全部频道
// （服务端每 ~54s 发 Ping，浏览器协议层自动回 Pong，无需应用层心跳）

export type WsStatus = 'idle' | 'connecting' | 'open' | 'closed'

export interface WsPayload {
  type?: string
  pairCode?: string
  interval?: string
  data?: Record<string, unknown> | unknown[]
  bids?: unknown
  asks?: unknown
  [key: string]: unknown
}

export interface WsChannel {
  channel: string
  payload: WsPayload
}

type StatusListener = (status: WsStatus) => void
type MessageListener = (msg: WsChannel) => void

const MAX_RETRY = 30000 // 30s 封顶
const MIN_RETRY = 1000

class WsClient {
  private ws: WebSocket | null = null
  private status: WsStatus = 'idle'
  private channels = new Set<string>()
  private statusListeners = new Set<StatusListener>()
  private messageListeners = new Set<MessageListener>()
  private retryMs = MIN_RETRY
  private retryTimer: number | null = null

  constructor() {
    // 页面可见性恢复时主动探测连接
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible' && this.status !== 'open' && this.channels.size > 0) {
        this.connect()
      }
    })
  }

  onStatus(cb: StatusListener): () => void {
    this.statusListeners.add(cb)
    cb(this.status)
    return () => this.statusListeners.delete(cb)
  }

  onMessage(cb: MessageListener): () => void {
    this.messageListeners.add(cb)
    return () => this.messageListeners.delete(cb)
  }

  private setStatus(s: WsStatus) {
    if (this.status === s) return
    this.status = s
    this.statusListeners.forEach((cb) => cb(s))
  }

  private url(): string {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${proto}://${location.host}/ws`
    const token = localStorage.getItem('auth_token')
    return token ? `${url}?token=${encodeURIComponent(token)}` : url
  }

  connect() {
    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) return
    this.setStatus('connecting')
    let ws: WebSocket
    try {
      ws = new WebSocket(this.url())
    } catch {
      this.scheduleReconnect()
      return
    }
    this.ws = ws

    ws.onopen = () => {
      this.retryMs = MIN_RETRY
      this.setStatus('open')
      // 重连成功后重订阅全部频道
      if (this.channels.size > 0) this.send({ cmd: 'subscribe', channels: [...this.channels] })
    }

    ws.onmessage = (ev) => this.handleMessage(ev.data)

    ws.onclose = () => {
      this.setStatus('closed')
      this.scheduleReconnect()
    }
    ws.onerror = () => {
      ws.close()
    }
  }

  private handleMessage(raw: unknown) {
    if (typeof raw !== 'string') return
    let payload: WsPayload
    try {
      payload = JSON.parse(raw) as WsPayload
    } catch {
      return
    }
    const channel = inferChannel(payload)
    if (!channel) return
    this.messageListeners.forEach((cb) => cb({ channel, payload }))
  }

  private scheduleReconnect() {
    if (this.retryTimer !== null) return
    const delay = Math.min(this.retryMs, MAX_RETRY)
    this.retryMs = Math.min(this.retryMs * 2, MAX_RETRY)
    this.retryTimer = window.setTimeout(() => {
      this.retryTimer = null
      this.connect()
    }, delay)
  }

  private send(obj: unknown) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(obj))
    }
  }

  subscribe(channels: string[]) {
    const fresh = channels.filter((c) => !this.channels.has(c))
    if (fresh.length === 0) return
    fresh.forEach((c) => this.channels.add(c))
    this.send({ cmd: 'subscribe', channels: fresh })
    if (this.status !== 'open') this.connect()
  }

  unsubscribe(channels: string[]) {
    const gone = channels.filter((c) => this.channels.delete(c))
    if (gone.length > 0) this.send({ cmd: 'unsubscribe', channels: gone })
  }

  disconnect() {
    this.retryMs = MIN_RETRY
    if (this.retryTimer !== null) {
      clearTimeout(this.retryTimer)
      this.retryTimer = null
    }
    if (this.ws) this.ws.close()
    this.ws = null
    this.setStatus('idle')
  }
}

// 按 payload 推断频道
export function inferChannel(p: WsPayload): string | null {
  const code = p.pairCode
  if (!code) return null
  switch (p.type) {
    case 'market.orderBook':
      return `depth.${code}`
    case 'market.candles':
      return p.interval ? `kline.${code}.${p.interval}` : null
    case 'market.fills':
      return `trade.${code}`
    case 'market.ticker':
      return `ticker.${code}`
    default:
      return null
  }
}

export const wsClient = new WsClient()
