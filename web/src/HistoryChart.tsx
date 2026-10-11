import { useMemo, useState } from 'react'
import {
  Bar, BarChart, CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis,
} from 'recharts'

type ChartPoint = { at: number; traffic: number }
type DisplayPoint = { at: number; traffic: number | null }

const HOUR = 60 * 60 * 1000
const DAY = 24 * HOUR

function startOfLocalDay(timestamp: number) {
  const date = new Date(timestamp)
  return new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime()
}

function buildTimeline(data: ChartPoint[], range: 'hourly' | 'daily') {
  const step = range === 'hourly' ? HOUR : DAY
  const slotCount = range === 'hourly' ? 25 : 31
  const latestPoint = data.reduce((latest, point) => Math.max(latest, point.at), 0)
  const currentSlot = range === 'hourly'
    ? Math.floor(Date.now() / HOUR) * HOUR
    : startOfLocalDay(Date.now())
  const latestSlot = range === 'hourly'
    ? Math.floor(latestPoint / HOUR) * HOUR
    : startOfLocalDay(latestPoint)
  const end = Math.max(currentSlot, latestSlot)
  const values = new Map(data.map((point) => [
    range === 'hourly' ? Math.floor(point.at / HOUR) * HOUR : startOfLocalDay(point.at),
    point.traffic,
  ]))

  return Array.from({ length: slotCount }, (_, index): DisplayPoint => {
    const offset = slotCount - 1 - index
    const day = new Date(end)
    day.setDate(day.getDate() - offset)
    const at = range === 'hourly' ? end - offset * step : day.getTime()
    return { at, traffic: values.get(at) ?? null }
  })
}

function hourLabel(timestamp: number) {
  return new Date(timestamp).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false })
}

function dayLabel(timestamp: number) {
  return new Date(timestamp).toLocaleDateString('zh-CN', { month: 'numeric', day: 'numeric' })
}

export default function HistoryChart({ data, range }: { data: ChartPoint[]; range: 'hourly' | 'daily' }) {
  const [chartWidth, setChartWidth] = useState(900)
  const timeline = useMemo(() => buildTimeline(data, range), [data, range])
  const sampleCount = timeline.filter((point) => point.traffic !== null).length
  const isCompact = chartWidth < 520
  const tickStep = range === 'hourly' ? (isCompact ? 6 : 4) : (isCompact ? 10 : 5)
  const ticks = timeline.filter((_, index) => index % tickStep === 0 || index === timeline.length - 1).map((point) => point.at)
  const formatLabel = range === 'hourly' ? hourLabel : dayLabel
  const tooltip = {
    contentStyle: {
      background: 'var(--chart-tooltip-bg)',
      border: '1px solid var(--line-strong)',
      borderRadius: 10,
      boxShadow: 'var(--shadow-popover)',
      fontSize: 12,
      color: 'var(--ink)',
    },
    formatter: (value: number | string) => [typeof value === 'number' ? value.toFixed(3) : value, '流量 (GB)'] as [string | number, string],
    labelFormatter: (value: number) => formatLabel(value),
  }
  const axis = { tickLine: false, axisLine: false, tick: { fill: 'var(--chart-axis)', fontSize: isCompact ? 10 : 11 } }
  const margin = isCompact
    ? { top: 10, right: 10, bottom: 2, left: 2 }
    : { top: 10, right: 18, bottom: 2, left: -8 }

  return (
    <ResponsiveContainer width="100%" height="100%" debounce={80} onResize={(width) => setChartWidth(width)}>
      {range === 'hourly' ? (
        <LineChart data={timeline} margin={margin}>
          <CartesianGrid stroke="var(--chart-grid)" vertical={false} />
          <XAxis
            dataKey="at"
            type="number"
            scale="time"
            domain={[timeline[0].at, timeline[timeline.length - 1].at]}
            allowDataOverflow
            ticks={ticks}
            tickFormatter={hourLabel}
            interval={0}
            minTickGap={isCompact ? 12 : 18}
            padding={{ left: isCompact ? 3 : 8, right: isCompact ? 3 : 8 }}
            {...axis}
          />
          <YAxis width={isCompact ? 40 : 48} tickFormatter={(value: number) => Number(value.toFixed(3)).toString()} {...axis} />
          <Tooltip {...tooltip} />
          <Line
            type="monotone"
            dataKey="traffic"
            connectNulls={false}
            isAnimationActive={false}
            stroke="var(--chart-line)"
            strokeWidth={2.25}
            dot={sampleCount <= 4 ? { r: 3, fill: 'var(--chart-line)', stroke: 'var(--modal)', strokeWidth: 2 } : false}
            activeDot={{ r: 4, fill: 'var(--chart-line)', stroke: 'var(--modal)', strokeWidth: 2 }}
          />
        </LineChart>
      ) : (
        <BarChart data={timeline} margin={margin}>
          <CartesianGrid stroke="var(--chart-grid)" vertical={false} />
          <XAxis
            dataKey="at"
            type="number"
            scale="time"
            domain={[timeline[0].at, timeline[timeline.length - 1].at]}
            allowDataOverflow
            ticks={ticks}
            tickFormatter={dayLabel}
            interval={0}
            minTickGap={isCompact ? 10 : 16}
            padding={{ left: isCompact ? 3 : 8, right: isCompact ? 3 : 8 }}
            {...axis}
          />
          <YAxis width={isCompact ? 40 : 48} tickFormatter={(value: number) => Number(value.toFixed(3)).toString()} {...axis} />
          <Tooltip {...tooltip} />
          <Bar dataKey="traffic" fill="var(--chart-line)" barSize={isCompact ? 12 : 20} maxBarSize={isCompact ? 16 : 26} isAnimationActive={false} radius={[4, 4, 0, 0]} />
        </BarChart>
      )}
    </ResponsiveContainer>
  )
}
