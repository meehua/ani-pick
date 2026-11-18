// 控件尺寸双档：移动端默认 44px 触控目标（h-11），lg+ 压回桌面密度。
// 全站从这组常量取值，不要再手写 "h-11 … lg:h-8" 组合。
export const ctl = {
  /** 条形紧凑控件：ToggleGroupItem、小按钮、SelectTrigger */
  bar: 'h-11 lg:h-8',
  /** 带标签行的输入 / 选择（桌面 h-9 与 Input 默认一致） */
  field: 'h-11 lg:h-9',
  /** 方形图标按钮 */
  square: 'size-11 lg:size-8',
  /** 可换行的多值触发器（TagField 全宽模式） */
  wrap: 'min-h-11 lg:min-h-9',
  /** 激活态用 primary 强调（覆盖 Toggle 组默认的 accent） */
  on: 'data-[state=on]:bg-primary data-[state=on]:text-primary-foreground',
} as const
