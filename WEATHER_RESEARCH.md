# 天气研究版

运行 `start-research.ps1`，打开 http://localhost:8789 。独立程序仅请求气象接口，使用 configs/research-stations.json 中的十二个站点，每轮结束十分钟后再次采集。既有交易模型未被此研究版修改。

## 第二版站点关注列表

2026-09-14 通过 NOAA AWC stationinfo 核对十二个 ICAO 站点及坐标；时区按站点所在地显式设置并由 Go IANA 时区库校验。包含纽约、釜山、香港、新加坡、东京、伦敦、巴黎、法兰克福、多伦多、迪拜、圣保罗和悉尼。这里的机场范围并非城市天气或市场结算站的自动映射。

分类首先检查两个模式是否覆盖目标日全部小时（按当地日长处理夏令时），预报接收时间是否在30分钟内，以及今天的最新实况是否在90分钟内。未通过归入“数据不足”；明天没有实况属于正常情况。

模式峰值差≥1.5°C或较上一轮峰值变化≥0.5°C，显示“变化明显”。至少三轮覆盖20分钟、峰值变化≤0.3°C且两模式峰值差<0.8°C，显示“趋于稳定”；其余为“继续观察”。这些阈值是可解释的界面导航规则，未经过准确性校准；相同预报批次被重复读取也可能满足短期稳定条件，不代表独立预报验证。所有状态仍保持相同采集频率，稳定站点不删除。

已实现：各站当地今天与明天、ECMWF IFS 0.25° 和 GFS seamless 小时温度、METAR 实况、峰值变化、缺测与来源标识、原始输入归档。两模式峰值差异不是预测区间；没有样本验证的情况下不显示人工设定的概率。

## 数据源与限制

- NOAA AWC API：全球航空站 METAR 实况，当前文档提供最长 30 天查询；多数端点单次最多 400 条。高频站点跨多天查询要分窗，否则每日峰值可能因截断而偏低。https://aviationweather.gov/data/api/
- NOAA NCEI ISD：长期全球逐小时站点观测及质量标记，需按站号、坐标、迁站记录核对，不能只凭城市名配对。小时样本最高值不必等于连续记录日最高值。https://www.ncei.noaa.gov/products/land-based-station/integrated-surface-database
- Open-Meteo：提供 ECMWF、NOAA 等模式的网格预报，属于预报分发服务，不能当作机场观测真值。https://open-meteo.com/en/docs
- Historical Forecast 将批次拼成连续序列；Previous Runs 的 previous_day1 是有效时刻前 24 小时的预报，并非对整个当地日期统一使用同一发布批次。严格日高验证应使用 Single Runs 原始批次并记录可用时间。https://open-meteo.com/en/docs/single-runs-api
- ERA5 等再分析包含事后信息，不能替代历史当时可取得的预报。数据许可、付费服务和历史覆盖范围应在批量下载前逐项核实。

未建立市场结算标签映射，不假设 METAR、地方气象台及天气网站完全同源。研究标签定义为“指定气象站、指定当地统计窗口、质量控制后的观测温度”，需要保留窗口及覆盖率。

## 离线误差校准验证

`node research-evaluate.mjs 输入.json 报告.json`

输入是 JSON 数组，每行字段：station、model、local_date、lead_hours、forecast_c、observed_c、issued_at、available_at、cutoff_at、target_start、target_end、observation_coverage、observation_source。其中 source 必须为 station_observation，时间字段使用带 UTC 偏移的 ISO 时间，lead_hours 为窗口开始与预测截止时刻的小时差。

脚本按站点／模式／提前量分组，按日期前 70% 训练、后 30% 验证；至少 20 个训练日期和 10 个验证日期。拒绝重复日期、预报晚于截止、观测覆盖率低于 90%、训练标签在验证截止尚不可用等输入。仅训练偏差修正并报告验证集 MAE 与经验区间覆盖率，不自动部署参数。

现已增加 `cmd/city-calibrate` 的城市日高试点：用 Open-Meteo Previous Runs 的 `previous_day1` 与 NOAA NCEI 同站 FM-15 METAR 日高配对，并按日期前70%训练、后30%留出。该数据只支持 PRE_DAY 约24小时提前量，不能用于证明同日盘准确率。2024-03-01至2025-08-20的试点中，KMIA、KAUS、KATL同时改善留出 MAE 与整数档对数损失并进入白名单；CYYZ变差、KDAL概率评分改善不足，均被拒绝。完整逐日样本和评分见 `data/backtest/city-weather-calibration.json`。

归档位于 data/weather-research/archive，当前无自动清理。展示更新历史保留本进程最近 144 轮；进程重启后图中更新历史重置，磁盘归档保留。电脑休眠期间不能采集。
