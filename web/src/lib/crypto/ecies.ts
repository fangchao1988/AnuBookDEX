// ECIES 订单加密（与 Go internal/dex/privacy/encryption.go 字节级一致）。
//
// 契约（由 Go 侧 privacy_ecies_test.go 固定，改任一侧都会导致另一侧测试失败）：
//   1. 曲线 P-256；临时公钥 = 33B 压缩点（SEC1）
//   2. 共享密钥 = SHA256(ECDH 共享点 X 坐标的 32B 定长大端)
//   3. AES-256-GCM，IV 12B，AAD = null
//   4. ViewTag = SHA256(引擎压缩公钥)[:4]      ← 由公钥派生，加密前即可算出
//   5. NoteCommitment = SHA256(nullifier || ciphertext || ephemeralPubKey)
//   6. 输出字段为 base64（对齐 Go []byte 的 JSON 编码）
//
// 注意：WebCrypto 的 AES-GCM 密文格式 = ciphertext || tag(16B)，
// 与 Go crypto/cipher 的 GCM.Seal 输出一致，故可直接互操作。

/** 引擎 ECIES 公钥信息（GET /api/v1/privacy/pubkey） */
export interface EciesPubkeyInfo {
  enabled: boolean
  pubkey?: string // '0x' + 33B 压缩点（base16）
  curve?: string // 'P-256'
  view_tag?: string // '0x' + 4B
  kdf?: string
  commitment?: string
}

/** 加密后的订单载荷，字段名与 Go privacy.EncryptedOrder 的 json tag 一致 */
export interface EncryptedOrderPayload {
  note_commitment: string // base64
  encrypted_data: string // base64
  ephemeral_pk: string // base64
  iv: string // base64
  view_tag: string // base64
  nullifier: string // base64
  deadline: number
  signature?: string // base64
}

const te = new TextEncoder()

function hexToBytes(hex: string): Uint8Array {
  const s = hex.replace(/^0x/, '')
  const out = new Uint8Array(s.length / 2)
  for (let i = 0; i < out.length; i++) out[i] = parseInt(s.slice(i * 2, i * 2 + 2), 16)
  return out
}

function toHex(b: Uint8Array): string {
  return Array.from(b)
    .map((x) => x.toString(16).padStart(2, '0'))
    .join('')
}

function toB64(b: Uint8Array): string {
  // 分块避免大文件 apply 栈溢出
  let s = ''
  const chunk = 0x8000
  for (let i = 0; i < b.length; i += chunk) {
    s += String.fromCharCode(...b.subarray(i, i + chunk))
  }
  return btoa(s)
}

function concat(...arrs: Uint8Array[]): Uint8Array {
  const total = arrs.reduce((n, a) => n + a.length, 0)
  const out = new Uint8Array(total)
  let o = 0
  for (const a of arrs) {
    out.set(a, o)
    o += a.length
  }
  return out
}

/** SHA-256 */
export async function sha256(data: Uint8Array): Promise<Uint8Array> {
  return new Uint8Array(await crypto.subtle.digest('SHA-256', data as unknown as BufferSource))
}

/**
 * 由引擎压缩公钥推导 ViewTag（契约 4）。
 * 加密方可预知 —— 这是「链下密文 + 引擎快速筛选」能工作的前提。
 */
export async function deriveViewTag(enginePubkeyCompressed: string): Promise<Uint8Array> {
  const pub = hexToBytes(enginePubkeyCompressed)
  const h = await sha256(pub)
  return h.slice(0, 4)
}

/** 构造订单明文 JSON（字段名必须与 Go privacy.unmarshalOrderFields 一致） */
export function orderPlaintext(f: {
  orderId: number
  side: 0 | 1
  type?: number
  price: string
  amount: string
  createAt?: number
  deadline: number
}): Uint8Array {
  return te.encode(
    JSON.stringify({
      order_id: f.orderId,
      buy_or_sell: f.side,
      type: f.type ?? 1, // 1 = Limit
      price: f.price,
      amount: f.amount,
      circuit_rate: '0',
      create_at: f.createAt ?? Date.now(),
      stp: 0,
      deadline: f.deadline,
    }),
  )
}

/**
 * 用引擎 ECIES 公钥加密订单，返回可直接 POST /order 的 ciphertext 字符串。
 * @param enginePubkeyCompressed '0x' + 33B P-256 压缩点
 */
export async function encryptOrder(
  enginePubkeyCompressed: string,
  plaintext: Uint8Array,
  nullifier: Uint8Array,
  deadline: number,
): Promise<EncryptedOrderPayload> {
  // 注意：ECDH 私钥不能用 importKey('raw', 32B)（raw 仅支持公钥），
  // 必须 generateKey 产出 CryptoKey 后使用。
  const eph = await crypto.subtle.generateKey({ name: 'ECDH', namedCurve: 'P-256' }, true, ['deriveBits'])

  // 1. 导入引擎公钥（33B 压缩点，raw 格式公钥可用）
  const pub = hexToBytes(enginePubkeyCompressed)
  const enginePub = await crypto.subtle.importKey(
    'raw',
    pub as unknown as BufferSource,
    { name: 'ECDH', namedCurve: 'P-256' },
    false,
    [],
  )

  // 2. ECDH -> 32B 共享点 X -> SHA256 -> AES 密钥（契约 2）
  const sharedBits = await crypto.subtle.deriveBits({ name: 'ECDH', public: enginePub }, eph.privateKey, 256)
  const sharedSecret = await sha256(new Uint8Array(sharedBits))
  const aesKey = await crypto.subtle.importKey('raw', sharedSecret as unknown as BufferSource, { name: 'AES-GCM' }, false, [
    'encrypt',
  ])

  // 3. AES-256-GCM（IV 12B，AAD null —— 契约 3）
  const iv = crypto.getRandomValues(new Uint8Array(12))
  const ctBuf = await crypto.subtle.encrypt({ name: 'AES-GCM', iv: iv as unknown as BufferSource }, aesKey, plaintext as unknown as BufferSource)
  const ciphertext = new Uint8Array(ctBuf)

  // 4. 临时公钥（33B 压缩点，契约 1）—— 导出为 SEC1 压缩格式
  const ephPubRaw = new Uint8Array(await crypto.subtle.exportKey('raw', eph.publicKey))
  const ephPubCompressed = await compressPoint(ephPubRaw)

  // 5. ViewTag 由引擎公钥派生（契约 4）
  const viewTag = await deriveViewTag(enginePubkeyCompressed)

  // 6. NoteCommitment = SHA256(nullifier || ciphertext || ephPK)（契约 5）
  const commitment = await sha256(concat(nullifier, ciphertext, ephPubCompressed))

  return {
    note_commitment: toB64(commitment),
    encrypted_data: toB64(ciphertext),
    ephemeral_pk: toB64(ephPubCompressed),
    iv: toB64(iv),
    view_tag: toB64(viewTag),
    nullifier: toB64(nullifier),
    deadline,
  }
}

/** 生成 nullifier（32B）：SHA256(orderId | trader | salt） */
export async function deriveNullifier(orderId: number, trader: string, salt?: Uint8Array): Promise<Uint8Array> {
  const s = salt ?? crypto.getRandomValues(new Uint8Array(16))
  return sha256(concat(te.encode(`${orderId}|${trader}|`), s))
}

/**
 * P-256 公钥压缩（65B uncompressed -> 33B compressed, SEC1）。
 * WebCrypto 只导出 65B 未压缩点，需自行压缩。
 */
async function compressPoint(uncompressed: Uint8Array): Promise<Uint8Array> {
  if (uncompressed.length === 33) return uncompressed
  if (uncompressed.length !== 65 || uncompressed[0] !== 0x04) {
    throw new Error('unexpected EC point format')
  }
  const x = uncompressed.slice(1, 33)
  const y = uncompressed.slice(33, 65)
  // y 的奇偶性由末位奇偶决定
  const out = new Uint8Array(33)
  out[0] = y[y.length - 1] & 1 ? 0x03 : 0x02
  out.set(x, 1)
  return out
}

/** 拉取引擎 ECIES 公钥；未配置 view key 时返回 { enabled: false } */
export async function fetchEciesPubkey(): Promise<EciesPubkeyInfo> {
  // Anubis 引擎独立部署，路径带 /anubis 前缀（nginx 去前缀后转发）
  const res = await fetch('/anubis/api/v1/privacy/pubkey')
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  return (await res.json()) as EciesPubkeyInfo
}

export const __testing = { hexToBytes, toHex, toB64, concat }
