import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { LeftNav } from './layout/LeftNav'
import { TopBar } from './layout/TopBar'
import { ModalHost } from './layout/ModalHost'
import TradePage from '../pages/TradePage'
import AssetsPage from '../pages/AssetsPage'
import LpPage from '../pages/LpPage'
import StrategyPage from '../pages/StrategyPage'
import DarkpoolPage from '../pages/DarkpoolPage'
import ActivityPage from '../pages/ActivityPage'

export default function App() {
  return (
    <BrowserRouter>
      <div className="flex h-full overflow-hidden">
        <LeftNav />
        <div className="flex-1 flex flex-col min-w-0 min-h-0">
          <TopBar />
          <main className="flex-1 min-h-0">
            <Routes>
              <Route path="/" element={<Navigate to="/trade/ALEOUSDCX" replace />} />
              <Route path="/trade/:symbol" element={<TradePage />} />
              <Route path="/assets" element={<AssetsPage />} />
              <Route path="/lp" element={<LpPage />} />
              <Route path="/strategy" element={<StrategyPage />} />
              <Route path="/darkpool" element={<DarkpoolPage />} />
              <Route path="/activity" element={<ActivityPage />} />
              <Route path="*" element={<Navigate to="/trade/ALEOUSDCX" replace />} />
            </Routes>
          </main>
        </div>
      </div>
      <ModalHost />
    </BrowserRouter>
  )
}
