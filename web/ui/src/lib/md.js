// A tiny Markdown renderer for release notes: ### headings, "- " lists (with
// indented continuation lines and nested items), **bold**, `code` and [links](url).
const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')

function inline(s) {
  return esc(s)
    .replace(/`([^`]+)`/g, '<code>$1</code>')
    .replace(/\*\*([^*]+)\*\*/g, '<b>$1</b>')
    .replace(/\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)/g, '<a href="$2" target="_blank" rel="noreferrer">$1</a>')
}

export function md(text) {
  const out = []
  let item = null
  let open = false
  const flush = () => { if (item !== null) { out.push(`<li>${inline(item)}</li>`); item = null } }
  for (const raw of (text || '').split('\n')) {
    const line = raw.replace(/\s+$/, '')
    const h = line.match(/^###\s+(.*)/)
    const li = line.match(/^\s*-\s+(.*)/)
    if (h) { flush(); if (open) { out.push('</ul>'); open = false } out.push(`<h4>${inline(h[1])}</h4>`) }
    else if (li) { flush(); if (!open) { out.push('<ul>'); open = true } item = li[1] }
    else if (line.trim() && item !== null) item += ' ' + line.trim()
    else if (line.trim()) { flush(); if (open) { out.push('</ul>'); open = false } out.push(`<p>${inline(line)}</p>`) }
  }
  flush()
  if (open) out.push('</ul>')
  return out.join('')
}
