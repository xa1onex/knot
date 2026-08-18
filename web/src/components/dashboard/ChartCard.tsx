import { motion } from 'framer-motion'
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'

export function ChartCard({
  title,
  description,
  data,
  dataKey = 'value',
  height = 300,
}: {
  title: string
  description: string
  data: Array<{ name: string; value: number }>
  dataKey?: string
  height?: number
}) {
  return (
    <motion.div
      whileHover={{ y: -4 }}
      transition={{ duration: 0.2 }}
      className="group relative overflow-hidden rounded-2xl border border-border/40 bg-background/60 p-6 backdrop-blur transition-all hover:border-border/60 hover:shadow-lg"
      role="article"
      aria-label={`${title}: ${description}`}
    >
      <div className="pointer-events-none absolute inset-0 -z-10 bg-gradient-to-br from-foreground/[0.04] via-transparent to-transparent opacity-0 transition-opacity duration-300 group-hover:opacity-100" />
      <div className="relative space-y-4">
        <div className="space-y-1">
          <h3 className="text-sm font-semibold uppercase tracking-[0.25em] text-foreground">{title}</h3>
          <p className="text-xs text-foreground/60">{description}</p>
        </div>
        <div className="relative" style={{ width: '100%', height }}>
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={data} margin={{ top: 5, right: 10, left: -25, bottom: 5 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" opacity={0.3} vertical={false} />
              <XAxis dataKey="name" stroke="hsl(var(--foreground))" opacity={0.6} style={{ fontSize: '11px' }} />
              <YAxis stroke="hsl(var(--foreground))" opacity={0.6} style={{ fontSize: '11px' }} width={35} />
              <Tooltip
                contentStyle={{
                  backgroundColor: 'hsl(var(--background))',
                  border: '1px solid hsl(var(--border))',
                  borderRadius: '8px',
                  padding: '8px 12px',
                }}
              />
              <Line
                type="natural"
                dataKey={dataKey}
                stroke="hsl(var(--primary))"
                strokeWidth={2.5}
                dot={false}
                activeDot={{ r: 6 }}
                isAnimationActive
                animationDuration={800}
              />
            </LineChart>
          </ResponsiveContainer>
        </div>
      </div>
    </motion.div>
  )
}
