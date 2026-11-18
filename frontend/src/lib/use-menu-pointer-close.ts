import * as React from 'react'

// Radix 菜单关闭后会把焦点还给触发按钮，focus-visible 光环残留其上。
// 鼠标/触控打开的菜单在关闭时不回焦；键盘打开保持默认回焦，Tab 导航不受影响。
// 用法：onPointerDown 给 DropdownMenuTrigger，onCloseAutoFocus 给 DropdownMenuContent。
export function useMenuPointerClose() {
  const openedByPointer = React.useRef(false)
  const onPointerDown = React.useCallback(() => {
    openedByPointer.current = true
  }, [])
  const onCloseAutoFocus = React.useCallback((e: Event) => {
    if (openedByPointer.current) e.preventDefault()
    openedByPointer.current = false
  }, [])
  return { onPointerDown, onCloseAutoFocus }
}
