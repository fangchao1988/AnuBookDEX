import { useEffect } from 'react'
import { toChannelSymbol } from '../lib/symbol'
import { subscribeChannels, unsubscribeChannels } from '../stores/marketStore'

// 订阅指定交易对的一组行情频道；组件卸载/参数变化时自动退订
// channels 为后端 interval 列表（如 ['1min','5min']），depth/trade/ticker 恒订阅
export function useMarketChannels(symbol: string, intervals: string[] = []) {
  const channelSymbol = toChannelSymbol(symbol)
  useEffect(() => {
    const channels = [
      `depth.${channelSymbol}`,
      `trade.${channelSymbol}`,
      `ticker.${channelSymbol}`,
      ...intervals.map((i) => `kline.${channelSymbol}.${i}`),
    ]
    subscribeChannels(channels)
    return () => unsubscribeChannels(channels)
  }, [channelSymbol, intervals.join(',')])
}
