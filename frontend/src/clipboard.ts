export async function copyText(text: string): Promise<boolean> {
  if (!text) return false

  // HTTPS / localhost 优先使用现代 Clipboard API。
  try {
    if (window.isSecureContext && navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch (_) {
    // 继续走兼容方案。
  }

  // HTTP IP 访问时，很多浏览器会禁用 navigator.clipboard。
  // 使用 textarea + execCommand 作为兼容复制方案，必须在用户点击事件里调用。
  try {
    const textarea = document.createElement('textarea')
    textarea.value = text
    textarea.setAttribute('readonly', 'readonly')
    textarea.style.position = 'fixed'
    textarea.style.left = '-9999px'
    textarea.style.top = '0'
    textarea.style.opacity = '0'
    document.body.appendChild(textarea)
    textarea.focus()
    textarea.select()
    textarea.setSelectionRange(0, textarea.value.length)
    const ok = document.execCommand('copy')
    document.body.removeChild(textarea)
    return ok
  } catch (_) {
    return false
  }
}
