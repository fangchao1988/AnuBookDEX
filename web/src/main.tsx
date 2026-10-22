import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './app/App'
import { useWallet } from './stores/wallet'
import { useChain } from './stores/chain'
import './index.css'

// 刷新页面后自动恢复钱包会话（已授权时不弹框）。
// 双链：Anubis 恢复 MetaMask（静默 eth_accounts），Aleo 恢复 Shield/Leo。
void useChain.getState().restore()
void useWallet.getState().restore()

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
