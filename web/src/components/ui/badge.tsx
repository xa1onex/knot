import type { HTMLAttributes } from 'react'
import { cn } from '@/lib/cn'

export function Badge({
  className,
  variant = 'outline',
  ...props
}: HTMLAttributes<HTMLSpanElement> & { variant?: 'outline' | 'default' }) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-2 rounded-full px-4 py-1.5 text-xs uppercase tracking-[0.2em]',
        variant === 'outline' &&
          'border border-border/50 bg-background/55 text-foreground/70 backdrop-blur',
        variant === 'default' && 'bg-primary text-primary-foreground',
        className,
      )}
      {...props}
    />
  )
}
