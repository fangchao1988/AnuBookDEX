import { StatCard } from '../components/ui/StatCard'
import { Tag } from '../components/ui/Tag'

// AI 智能策略引擎：对齐原型 #strategy-view
export default function StrategyPage() {
  return (
    <div className="p-5 overflow-y-auto h-full">
      <h2 className="text-xl font-semibold mb-5">AI 智能策略引擎</h2>
      <div className="grid gap-3 grid-cols-[repeat(auto-fill,minmax(240px,1fr))]">
        <StatCard
          title="行情研判"
          value={<span className="inline-flex items-center gap-1 px-2 py-0.5 rounded bg-up-bg text-up text-[11px] font-semibold">看涨 BTC</span>}
          sub="置信度 78% · 更新于 14:30"
        />
        <StatCard title="今日AI交易" value="12 笔" sub="冰山拆分 8 · 点位推荐 4" />
        <StatCard title="AI风控动作" value="2 次" sub="预警 1 · 减仓提醒 1" />
        <StatCard title="策略运行" value="1 个活跃" sub="离线隐私托管 · 策略不泄露" valueClassName="text-cyan" />
      </div>

      <h3 className="text-[15px] text-text-secondary mt-6 mb-3 border-b border-line pb-2">AI 风控实时监控</h3>
      <div className="bg-bg-secondary border border-line rounded-lg overflow-hidden">
        <table className="w-full border-collapse text-[13px]">
          <thead>
            <tr>
              {['交易对', '方向', '保证金率', '强平价格', '当前价格', '风险等级', 'AI动作'].map((h) => (
                <th key={h} className="bg-bg-tertiary text-text-muted font-normal px-4 py-2 text-left text-[11px]">
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            <tr>
              <td className="px-4 py-2 border-t border-line">BTC/USDT</td>
              <td className="px-4 py-2 border-t border-line text-up">多头 3x</td>
              <td className="px-4 py-2 border-t border-line">33.3%</td>
              <td className="px-4 py-2 border-t border-line">56,400</td>
              <td className="px-4 py-2 border-t border-line">68,245</td>
              <td className="px-4 py-2 border-t border-line"><Tag variant="green">安全</Tag></td>
              <td className="px-4 py-2 border-t border-line">—</td>
            </tr>
            <tr>
              <td className="px-4 py-2 border-t border-line">ETH/USDT</td>
              <td className="px-4 py-2 border-t border-line text-down">空头 3x</td>
              <td className="px-4 py-2 border-t border-line">33.3%</td>
              <td className="px-4 py-2 border-t border-line">4,200</td>
              <td className="px-4 py-2 border-t border-line">3,480</td>
              <td className="px-4 py-2 border-t border-line"><Tag variant="green">安全</Tag></td>
              <td className="px-4 py-2 border-t border-line">—</td>
            </tr>
            <tr className="bg-orange/5">
              <td className="px-4 py-2 border-t border-line">BTC/USDT</td>
              <td className="px-4 py-2 border-t border-line text-up">多头 3x</td>
              <td className="px-4 py-2 border-t border-line">18.2%</td>
              <td className="px-4 py-2 border-t border-line">60,500</td>
              <td className="px-4 py-2 border-t border-line">68,245</td>
              <td className="px-4 py-2 border-t border-line"><Tag variant="orange">警示 82%</Tag></td>
              <td className="px-4 py-2 border-t border-line text-[11px] text-orange">AI建议: 部分减仓</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div className="mt-2 p-2.5 bg-down/5 border border-down/20 rounded-md text-xs">
        <b className="text-down">AI风控规则:</b> 80%风险线=弹窗提醒 · 90%高危线=AI自动减仓(<b className="text-text-primary">默认关闭</b>，启用后可在设置中配置阈值/比例/次数等边界) · 接近强平线=自动对冲/平仓
      </div>

      <h3 className="text-[15px] text-text-secondary mt-6 mb-3 border-b border-line pb-2">策略离线隐私托管</h3>
      <div className="bg-bg-secondary border border-line rounded-lg overflow-hidden">
        <table className="w-full border-collapse text-[13px]">
          <thead>
            <tr>
              {['策略名称', '状态', '类型', '创建时间', '最近运行', '操作'].map((h) => (
                <th key={h} className="bg-bg-tertiary text-text-muted font-normal px-4 py-2 text-left text-[11px]">
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            <tr>
              <td className="px-4 py-2 border-t border-line">BTC网格策略 v2</td>
              <td className="px-4 py-2 border-t border-line"><Tag variant="green">运行中</Tag></td>
              <td className="px-4 py-2 border-t border-line">网格交易</td>
              <td className="px-4 py-2 border-t border-line">2026-05-20</td>
              <td className="px-4 py-2 border-t border-line">14:30:15</td>
              <td className="px-4 py-2 border-t border-line text-xs"><a href="#" className="text-blue">暂停</a> · <a href="#" className="text-blue">日志</a> · <a href="#" className="text-blue">快照</a></td>
            </tr>
            <tr>
              <td className="px-4 py-2 border-t border-line">ETH趋势跟踪</td>
              <td className="px-4 py-2 border-t border-line"><Tag variant="red">已停止</Tag></td>
              <td className="px-4 py-2 border-t border-line">趋势策略</td>
              <td className="px-4 py-2 border-t border-line">2026-05-15</td>
              <td className="px-4 py-2 border-t border-line">05-27 18:00</td>
              <td className="px-4 py-2 border-t border-line text-xs"><a href="#" className="text-blue">启动</a> · <a href="#" className="text-blue">编辑</a> · <a href="#" className="text-blue">删除</a></td>
            </tr>
          </tbody>
        </table>
      </div>
      <div className="mt-3 flex gap-3">
        <button className="bg-purple text-white border-none px-6 py-2.5 rounded-md font-semibold text-sm cursor-pointer hover:opacity-90">+ 新建策略</button>
        <button className="bg-bg-tertiary text-text-primary border-none px-6 py-2.5 rounded-md font-semibold text-sm cursor-pointer hover:opacity-90">上传策略脚本</button>
        <button className="bg-bg-tertiary text-text-primary border-none px-6 py-2.5 rounded-md font-semibold text-sm cursor-pointer hover:opacity-90">策略快照备份</button>
      </div>
      <div className="mt-2 text-[11px] text-text-muted">策略代码离线运行 · 不上链不落地明文 · 仅加密交易指令上链 · 权限隔离</div>

      <h3 className="text-[15px] text-text-secondary mt-6 mb-3 border-b border-line pb-2">AI 智能交易路由</h3>
      <div className="bg-bg-secondary border border-line rounded-lg p-4">
        <div className="font-bold mb-2">路由状态面板</div>
        <RouteRow label="AleoBook 订单簿深度" value="优 (买卖价差 0.02%)" up />
        <RouteRow label="RocketSwap AMM 深度" value="中等 (滑点 0.15%)" />
        <RouteRow label="AI推荐路径" value="AleoBook 订单簿 (节省 0.13%)" up bold />
        <RouteRow label="预计滑点" value="< 0.01%" up />
      </div>
    </div>
  )
}

function RouteRow({ label, value, up, bold }: { label: string; value: string; up?: boolean; bold?: boolean }) {
  return (
    <div className={`py-1.5 px-2.5 border-b border-line flex justify-between text-[11px] last:border-b-0 ${bold ? 'font-semibold' : ''}`}>
      <span className="text-text-muted">{label}</span>
      <span className={up ? 'text-up' : 'text-text-secondary'}>{value}</span>
    </div>
  )
}
