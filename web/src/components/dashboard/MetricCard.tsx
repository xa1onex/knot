import type { ReactNode } from 'react'
import { motion } from 'framer-motion'
import { TrendingDown, TrendingUp } from 'lucide-react'

export function MetricCard({
  label,
  value,
  change,
  trend,
  icon,
}: {
  label: string
  value: string
  change?: string
  trend?: 'up' | 'down' | 'neutral'
  icon: ReactNode
}) {
  const TrendIcon = trend === 'down' ? TrendingDown : TrendingUp
  const isPositive = trend === 'up'

  return (
    <motion.div
      whileHover={{ y: -4 }}
      transition={{ duration: 0.2 }}
      className="group relative overflow-hidden rounded-2xl border border-border/40 bg-background/60 p-6 backdrop-blur transition-all hover:border-border/60 hover:shadow-lg"
      role="article"
      aria-label={`${label}: ${value}`}
    >
      <div className="pointer-events-none absolute inset-0 -z-10 bg-gradient-to-br from-foreground/[0.04] via-transparent to-transparent opacity-0 transition-opacity duration-300 group-hover:opacity-100" />
      <div className="relative space-y-4">
        <div className="flex items-center justify-between">
          <div className="text-2xl" aria-hidden>
            {icon}
          </div>
          {change && trend && trend !== 'neutral' && (
            <div
              className={`flex items-center gap-1 rounded-full px-2 py-1 text-xs font-semibold ${
                isPositive
                  ? 'bg-emerald-500/20 text-emerald-600'
                  : 'bg-red-500/20 text-red-600'
              }`}
            >
              <TrendIcon className="h-3 w-3" aria-hidden />
              {change}
            </div>
          )}
        </div>
        <div className="space-y-1">
          <p className="text-xs font-semibold uppercase tracking-[0.25em] text-foreground/40">{label}</p>
          <p className="text-2xl font-semibold tracking-tight text-foreground">{value}</p>
        </div>
      </div>
    </motion.div>
  )
}
