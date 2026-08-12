import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './app/App'
import { useWallet } from './stores/wallet'
import './index.css'

// 刷新页面后自动恢复钱包会话（已授权时不弹框）
void useWallet.getState().restore()

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
