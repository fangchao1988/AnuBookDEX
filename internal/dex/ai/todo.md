"AI"在架构中的真实角色
根据改造方案和 PRD 的定位，当前是 MVP 占位实现，真正的 AI 能力规划在后续迭代：


// engine.go:60 - 舆情接口已预留，但只是内存 map
sentiment map[string]float64 // 外部数据源接入（当前为手动 SetSentiment）

// iceberg.go:239 - Adaptive 策略目前就是固定 5%
pct := decimal.NewFromFloat(0.05) // 未来可替换为 ML 模型输出

// risk.go:229 - 风险判定是固定阈值
case distance < 0.01: return RiskCritical // 未来可替换为概率模型
架构意图是：三个组件都设计了回调 + 外部接口，为将来接入真正的 AI 模型（强化学习做冰山拆分、LSTM/Transformer 做价格预测、GBDT 做风控评分）留好了插拔点，但当前版本是纯规则引擎。