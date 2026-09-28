import { cva, type VariantProps } from 'class-variance-authority'

export { default as Button } from './Button.vue'

// shadcn-vue 的按钮变体，配色全部走主题令牌。
export const buttonVariants = cva(
  'relative inline-flex select-none items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium ' +
    'transition-all duration-200 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 ' +
    'focus-visible:ring-offset-background active:scale-[0.97] disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 ' +
    '[&_svg]:size-4 [&_svg]:shrink-0 cursor-pointer',
  {
    variants: {
      variant: {
        default:
          'bg-gradient-to-r from-primary to-accent text-primary-foreground shadow-[0_0_20px_-6px_hsl(var(--primary)/0.7)] hover:shadow-[0_0_28px_-4px_hsl(var(--primary)/0.85)] hover:brightness-110',
        secondary: 'bg-secondary/15 text-secondary hover:bg-secondary/25 border border-secondary/30',
        outline: 'border border-primary/30 bg-transparent text-foreground hover:border-primary/60 hover:bg-primary/10',
        ghost: 'text-muted-foreground hover:bg-primary/10 hover:text-foreground',
        destructive: 'bg-destructive/90 text-destructive-foreground hover:bg-destructive',
        link: 'text-primary underline-offset-4 hover:underline',
      },
      size: {
        default: 'h-9 px-4 py-2',
        sm: 'h-8 rounded-md px-3 text-xs',
        lg: 'h-11 rounded-lg px-6 text-base',
        icon: 'h-9 w-9',
      },
    },
    defaultVariants: { variant: 'default', size: 'default' },
  },
)

export type ButtonVariants = VariantProps<typeof buttonVariants>
