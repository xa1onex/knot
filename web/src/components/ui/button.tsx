import type { ButtonHTMLAttributes } from 'react'
import { cn } from '@/lib/cn'

type Props = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: 'default' | 'outline' | 'ghost'
  size?: 'default' | 'sm' | 'icon'
}

export function Button({
  className,
  variant = 'default',
  size = 'default',
  type = 'button',
  ...props
}: Props) {
  return (
    <button
      type={type}
      className={cn(
        'inline-flex items-center justify-center gap-2 rounded-lg font-medium transition-colors disabled:pointer-events-none disabled:opacity-40',
        variant === 'default' && 'bg-primary text-primary-foreground hover:bg-primary/90',
        variant === 'outline' &&
          'border border-border/40 bg-background/60 backdrop-blur hover:border-border/60 hover:bg-background/70',
        variant === 'ghost' && 'text-foreground/70 hover:bg-background/50 hover:text-foreground',
        size === 'default' && 'h-10 px-4 text-sm',
        size === 'sm' && 'h-8 px-3 text-xs',
        size === 'icon' && 'h-10 w-10 rounded-full',
        className,
      )}
      {...props}
    />
  )
}
