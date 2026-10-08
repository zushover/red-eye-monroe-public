# Red Eye Monroe · 红眼曼罗

[English](README.md) | [简体中文](README.zh-CN.md)

一个运行在本地的天气市场研究平台，将气象站实况、多源天气预报、Polymarket 盘口、概率诊断和持仓监测整合到同一个界面。

项目使用 Go、JavaScript 和 PowerShell 构建，提供全球城市概览、市场矩阵、策略研究、模拟持仓和可选的实盘执行接口。

## 主要功能

- **全球概览**：地球仪、昼夜显示，以及各城市的天气与市场详情。
- **多源天气对比**：ECMWF、GFS、Open-Meteo 和 METAR 实况，支持温度曲线与气象站关系诊断。
- **城市概率模型**：按城市订正预报偏差，研究经验误差分布、概率分解和时间顺序验证。
- **市场矩阵**：城市搜索、排序、当地日期选择；以摄氏度展示，同时保留市场原始华氏温度档位对应关系。
- **策略与持仓研究**：模拟交易、执行状态核对、动态退出规则和组合资金核算。
- **可选钱包集成**：使用 Windows DPAPI 在本机加密保存凭证，并通过明确的会话授权控制实盘执行。

## 快速开始

运行环境为 Windows 和 PowerShell。首次启动时，构建脚本会按需下载便携版 Go 工具链，并校验 SHA256。运行 JavaScript 测试、研究脚本或钱包执行模块时还需要安装 Node.js。

```powershell
git clone https://github.com/zushover/red-eye-monroe-public.git
cd red-eye-monroe-public
Copy-Item config.example.json config.json
./start.ps1
```

启动后打开 [localhost:8787](http://localhost:8787)。示例配置默认使用模拟模式，读取公开天气和市场数据不需要钱包凭证。

需要后台采集时，运行 `./background.ps1`；停止时运行 `./stop.ps1`。运行状态会保存在本地 `data/` 目录。停止后如需恢复，应先在本地清除停止标记。持续采集期间需要保持电脑开机、联网且不休眠。

## 测试与构建

```powershell
./scripts/bootstrap.ps1
node --test dashboard-selection.test.mjs research-evaluate.test.mjs
cd execution-sdk
npm ci
node --test *.test.mjs
```

构建脚本会运行 Go 测试并编译采集程序。离线测试不需要配置账户凭证。

## 天气概率模型研究

研究代码按气象站和当地日期评估预报误差，将城市模型与基线进行比较，并分别检验概率校准效果和交易收益。报告包含预测与实况对比、Brier 分数、对数损失、前向验证，以及天气概率与市场概率的分解。

- [研究方法与离线升级方案](docs/weather-probability-research.md)
- [城市模型审计](docs/model-audit.html)
- [后续独立天气数据验证](docs/forward-weather-audit.html)
- [天气与市场概率分解](docs/probability-decomposition.html)
- [天气概率 V2 审计](docs/weather-v2-audit.html)
- [气象站研究流程](WEATHER_RESEARCH.md)

HTML 报告是静态研究快照，可在克隆仓库后本地打开。公开仓库不包含原始历史数据、运行归档、账户数据和订单记录；重新运行研究脚本前，需要自行采集或提供对应输入。

部分城市模型改善了温度预测误差，但目前的研究尚未证明稳定的交易盈利能力。历史预报的提前量口径，以及由 METAR 推导的日最高温，与市场精确结算规则之间仍存在需要验证的差异。

## 可选实盘执行接口

源码包含钱包检查、订单约束、买单状态核对、卖单重试和成交核算。先安装执行模块依赖：

```powershell
cd execution-sdk
npm ci
```

配置执行会话前，将 `config.live.example.json` 复制为本地 `config.live.json`。使用 `setup-wallet.ps1` 在自己的电脑上设置凭证，凭证会为当前 Windows 用户加密保存。`check-wallet.ps1` 和 `check-live.ps1` 用于账户检查。

`start-live.ps1` 需要本地实盘配置、成功的启动前检查和明确的会话启用操作。公开版不包含钱包、凭证、账户授权或已启用的执行会话。

## 项目结构

```text
cmd/             采集、校准和研究程序入口
internal/        天气客户端、模型、定价、可视化和执行逻辑
execution-sdk/   钱包适配、订单核对、资金核算和测试
configs/         公开气象站信息与校准参数
scripts/         构建及后台运行辅助脚本
docs/            静态研究报告与方法说明
research-*.mjs   离线分析和报告生成脚本
```

## 隐私与配置

此仓库是重新导出的纯净源码版本，不包含原项目提交历史、本地钱包存储、API 凭据、个人网络配置、交易账本和账户快照。本地配置及生成数据已通过 `.gitignore` 排除。

代码保留公开数据服务地址和凭证字段名称，供集成接口使用。需要的凭证应在自己的电脑上配置，不要提交到仓库。
