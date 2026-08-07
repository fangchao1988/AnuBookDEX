import { StatCard } from '../components/ui/StatCard'
import { Tag } from '../components/ui/Tag'

// 资产总览：对齐原型 #assets-view
export default function AssetsPage() {
  return (
    <div className="p-5 overflow-y-auto h-full">
      <h2 className="text-xl font-semibold mb-5">资产总览</h2>
      <div className="grid gap-3 grid-cols-[repeat(auto-fill,minmax(240px,1fr))]">
        <StatCard title="总资产估值" value="$ 152,634.52" sub="≈ 2.24 BTC" valueClassName="text-up" />
        <StatCard title="现货余额" value="$ 98,234.16" sub="可用: 86,450.00 USDT + 0.1734 BTC" />
        <StatCard title="杠杆持仓" value="$ 45,400.36" sub="3x 杠杆 · 保证金: 15,133.45 USDT" valueClassName="text-orange" />
        <StatCard title="流动性资产" value="$ 9,000.00" sub="BTC/USDT LP · 累计收益 +$320.50" valueClassName="text-blue" />
        <StatCard title="累计盈亏 (7天)" value="+$3,456.78" sub="+2.32% · 胜率 62%" valueClassName="text-up" />
        <StatCard
          title={<><span className="text-purple">NFT</span> 权益</>}
          value="ALEO OG NFT"
          valueClassName="text-purple text-base"
          sub="手续费-20% · AI折扣 · 杠杆利率优惠"
          className="border-purple/30 bg-purple/5"
        />
      </div>

      <h3 className="text-[15px] text-text-secondary mt-6 mb-3 border-b border-line pb-2">现货资产明细</h3>
      <DataTable
        heads={['币种', '总量', '可用', '冻结中', '折合 USDT', '操作']}
        rows={[
          ['USDT', '86,450.00', '86,450.00', '0.00', '$86,450.00', <a href="#" className="text-blue">充提</a>],
          ['BTC', '0.5234', '0.1734', '0.3500', '$35,698.16', <a href="#" className="text-blue">充提</a>],
          ['ETH', '8.2000', '5.0000', '3.2000', '$28,536.00', <a href="#" className="text-blue">充提</a>],
          ['ALEO', '50,000', '50,000', '0', '$1,950.00', <a href="#" className="text-blue">充提</a>],
        ]}
      />

      <h3 className="text-[15px] text-text-secondary mt-6 mb-3 border-b border-line pb-2">交易账单</h3>
      <DataTable
        heads={['时间', '类型', '交易对', '金额', '手续费', '利息', '状态']}
        rows={[
          ['2026-05-28 14:15', <Tag variant="green">买入</Tag>, 'BTC/USDT', '13,578.00 USDT', <><span className="font-mono">13.57 USDT</span> <span className="text-[10px] text-purple">(NFT-20%)</span></>, '-', <Tag variant="green">成功</Tag>],
          ['2026-05-28 10:30', <Tag variant="red">卖出</Tag>, 'ETH/USDT', '11,040.00 USDT', '11.04 USDT', '-', <Tag variant="green">成功</Tag>],
          ['2026-05-27 22:00', <Tag variant="blue">杠杆利息</Tag>, 'BTC/USDT', '-2.34 USDT', '-', '2.34 USDT', <Tag variant="green">成功</Tag>],
          ['2026-05-27 18:45', <Tag variant="purple">暗池</Tag>, 'ETH/USDT', '50,000 USDT', '100.00 USDT', '-', <Tag variant="green">成功</Tag>],
        ]}
      />
      <div className="mt-3 text-xs text-text-muted">
        所有日志支持导出 · 本地加密保存 · <a href="#" className="text-blue">导出CSV</a> · <a href="#" className="text-blue">导出PDF</a>
      </div>
    </div>
  )
}

export function DataTable({
  heads,
  rows,
}: {
  heads: string[]
  rows: (string | JSX.Element)[][]
}) {
  return (
    <div className="bg-bg-secondary border border-line rounded-lg overflow-hidden">
      <table className="w-full border-collapse text-[13px]">
        <thead>
          <tr>
            {heads.map((h) => (
              <th key={h} className="bg-bg-tertiary text-text-muted font-normal px-4 py-2 text-left text-[11px]">
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i} className="hover:bg-bg-hover">
              {r.map((c, j) => (
                <td key={j} className="px-4 py-2 border-t border-line">
                  {c}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
