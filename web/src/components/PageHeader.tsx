import type { ReactNode } from 'react'

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
    <header className="page-header">
      <div className="page-header-copy">
        {kicker && <p className="page-kicker">{kicker}</p>}
        {live != null && (
          <span className={`live-badge ${live ? 'on' : 'off'}`}>
            <i aria-hidden />
            {live ? liveLabel : 'Waiting'}
          </span>
        )}
        <h1>{title}</h1>
        {description && <p className="page-lead">{description}</p>}
      </div>
      {actions && <div className="page-header-actions">{actions}</div>}
    </header>
  )
}
