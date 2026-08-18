import type { ReactNode } from 'react'
import { motion } from 'framer-motion'

export default function PageHeader({
  kicker,
  title,
  description,
  live,
  liveLabel = 'Live',
  actions,
}: {
  kicker?: string
  title: string
  description?: string
  live?: boolean
  liveLabel?: string
  actions?: ReactNode
}) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 20 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.5 }}
      className="mb-12 space-y-4"
    >
      <div className="flex flex-col justify-between gap-4 md:flex-row md:items-center">
        <div className="space-y-2">
          {kicker && (
            <p className="text-xs font-semibold uppercase tracking-[0.25em] text-foreground/40">{kicker}</p>
          )}
          {live != null && (
            <span className="inline-flex items-center gap-2 rounded-full border border-border/50 bg-background/55 px-4 py-1.5 text-xs uppercase tracking-[0.2em] text-foreground/70 backdrop-blur">
              <span className={`h-2 w-2 rounded-full ${live ? 'bg-emerald-500' : 'bg-foreground/30'}`} />
              {liveLabel}
            </span>
          )}
          <h1 className="text-balance text-3xl font-semibold tracking-tight text-foreground md:text-4xl">
            {title}
          </h1>
          {description && <p className="max-w-2xl text-foreground/70">{description}</p>}
        </div>
        {actions && <div className="flex gap-2">{actions}</div>}
      </div>
    </motion.div>
  )
}
